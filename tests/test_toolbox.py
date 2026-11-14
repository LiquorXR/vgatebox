"""Behavior tests use synthetic observations; they never claim a real VPN succeeded."""
import base64
import csv
import io
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "src"))
from vpngate_toolbox.classifier import classify
from vpngate_toolbox.cli import main
from vpngate_toolbox.common import ServiceError, ToolError, digest
from vpngate_toolbox.config import load_settings
from vpngate_toolbox.pool import build_pool
from vpngate_toolbox.vpngate import parse_csv, profile_remotes

SETTINGS = load_settings(ROOT / "config/settings.json")
EXIT = "73.1.1.1"


def csv_data(ip="8.8.8.8", proto="tcp", score=100):
    stream = io.StringIO()
    stream.write("*vpn_servers\n")
    writer = csv.writer(stream)
    writer.writerow(["#HostName", "IP", "Score", "Ping", "CountryShort", "OpenVPN_ConfigData_Base64"])
    profile = f"client\ndev tun\nproto {proto}\nremote {ip} 1194\n<ca>\nremote invalid.example 9\n</ca>\n"
    writer.writerow(["vpn-test", ip, score, "-", "US", base64.b64encode(profile.encode()).decode()])
    stream.write("*\n")
    return stream.getvalue().encode()


def image():
    return {"ip": EXIT, "country_code": "US", "asn": {"asn": "AS7922", "name": "Comcast", "type": "isp"},
            "privacy": {"is_hosting": False}}


class SourceTests(unittest.TestCase):
    def test_pool_cli_errors_preserve_pool_and_hide_private_details(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'data'
            output.mkdir()
            previous = output / 'node_pool.json'
            previous.write_bytes(b'{"nodes":[{"existing":"private"}]}')
            args = ['--config', str(ROOT / 'config/settings.json'), '--env-file', str(root / 'none'),
                    'pool', '--output', str(output)]
            for error, status in ((ServiceError('pool_source_unavailable'), 2),
                                  (ToolError('invalid_setting_node_timeout_seconds'), 2),
                                  (OSError('private filesystem failure'), 2)):
                with self.subTest(error=type(error).__name__), patch('vpngate_toolbox.cli.build_pool', side_effect=error), \
                        patch('sys.stderr', new_callable=io.StringIO) as stderr:
                    self.assertEqual(main(args), status)
                    self.assertNotIn('private filesystem', stderr.getvalue())
                    self.assertEqual(previous.read_bytes(), b'{"nodes":[{"existing":"private"}]}')

    def test_id_stays_stable_when_ip_and_profile_change(self):
        first = parse_csv(csv_data(), SETTINGS)[0][0]
        second = parse_csv(csv_data(ip="1.1.1.1"), SETTINGS)[0][0]
        self.assertEqual(first["node_id"], second["node_id"])
        self.assertNotEqual(first["protocols"]["openvpn"]["profile_sha256"], second["protocols"]["openvpn"]["profile_sha256"])
        self.assertEqual(second["protocols"]["openvpn"]["status"], "not_tested")
        self.assertIsNone(second["protocols"]["sstp"]["expected_exit_ip"])

    def test_discovery_is_not_truncated_by_probe_budget(self):
        data = csv_data().decode().replace("*\n", "")
        row = data.splitlines()[-1].replace("vpn-test", "vpn-other")
        data += row + "\n*\n"
        nodes, profiles, _ = parse_csv(data.encode(), {**SETTINGS, "candidate_limit": 1})
        self.assertEqual(len(nodes), 2)
        self.assertTrue(all(n["protocols"]["openvpn"]["status"] == "not_tested" for n in nodes))
        self.assertEqual(len(profiles), 1)

    def test_udp_port_is_not_an_sstp_capability(self):
        value = parse_csv(csv_data(proto="udp"), SETTINGS)[0][0]
        self.assertEqual([c["port"] for c in value["protocols"]["sstp"]["candidates"]], [443])
        self.assertEqual(value["protocols"]["sstp"]["status"], "not_tested")

    def test_inline_material_is_not_parsed_as_commands(self):
        self.assertEqual(profile_remotes("proto tcp\nremote 8.8.8.8 443\n<ca>\nremote evil 99\n</ca>"),
                         [{"host": "8.8.8.8", "port": 443, "transport": "tcp"}])

    def test_private_and_malformed_source_rejected(self):
        nodes, _, rejected = parse_csv(csv_data(ip="127.0.0.1"), SETTINGS)
        self.assertEqual(nodes, [])
        self.assertEqual(rejected, 1)
        with self.assertRaises(ToolError):
            parse_csv(b"<html>502</html>", SETTINGS)

    def test_pool_keeps_profiles_and_digest(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)
            fixture = path / "source.csv"
            fixture.write_bytes(csv_data())
            with patch.dict(os.environ, {}, clear=True):
                pool = build_pool(SETTINGS, path / "data", fixture)
            self.assertEqual(len(pool["nodes"]), 1)
            protocol = pool["nodes"][0]["protocols"]["openvpn"]
            self.assertEqual(digest((path / "data" / protocol["profile_ref"]).read_bytes()), protocol["profile_sha256"])
            self.assertEqual((path / "data/node_pool.json.sha256").read_text().strip(), digest((path / "data/node_pool.json").read_bytes()))


class ClassifierTests(unittest.TestCase):
    def test_residential_requires_explicit_evidence(self):
        raw = image()
        self.assertEqual(classify(raw, "test", EXIT)["ip_type"], "unknown")
        raw["privacy"]["is_residential"] = True
        self.assertEqual(classify(raw, "test", EXIT)["ip_type"], "strict_residential")

    def test_missing_hosting_is_not_false(self):
        raw = image()
        raw["privacy"] = {}
        raw["is_datacenter"] = False  # Old Checker synthesized flag is intentionally ignored.
        result = classify(raw, "test", EXIT)
        self.assertEqual(result["ip_type"], "unknown")
        self.assertIsNone(result["hosting"])
        self.assertIsNone(result["proxy"])

    def test_company_hosting_blocks_residential_label(self):
        raw = image()
        raw["company"] = {"type": "hosting"}
        self.assertEqual(classify(raw, "test", EXIT)["ip_type"], "unknown")
        raw["privacy"]["is_residential"] = True
        self.assertEqual(classify(raw, "test", EXIT)["ip_type"], "unknown")

    def test_explicit_negative_residential_is_not_residential(self):
        raw = image()
        raw["privacy"]["is_residential"] = False
        self.assertEqual(classify(raw, "test", EXIT)["ip_type"], "unknown")

    def test_intelligence_must_describe_queried_ip(self):
        with self.assertRaisesRegex(ToolError, "intelligence_ip_mismatch"):
            classify(image(), "test", "1.1.1.1")


if __name__ == '__main__':
    unittest.main()
