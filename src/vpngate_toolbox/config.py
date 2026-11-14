from __future__ import annotations

import json
import os
import re
from pathlib import Path

from .common import ToolError, https_url

OFFICIAL_PRIMARY_URL = 'https://www.vpngate.net/api/iphone/'
OFFICIAL_BACKUP_URL = 'https://download.vpngate.jp/api/iphone/'


def load_env(path: Path):
    """Read simple KEY=VALUE files without executing shell syntax."""
    if not path.exists():
        return
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        key, separator, value = line.partition("=")
        if not separator or not re.fullmatch(r"[A-Z][A-Z0-9_]*", key.strip()):
            raise ToolError("invalid_env_file")
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
        os.environ.setdefault(key.strip(), value)


def load_settings(path: Path) -> dict:
    settings = json.loads(path.read_text())
    settings.setdefault('pool_source_timeout_seconds', 10)
    settings.setdefault('mirror_fallback', {'enabled': True, 'max_age_hours': 24})
    source_timeout = settings['pool_source_timeout_seconds']
    mirror = settings['mirror_fallback']
    if type(source_timeout) is not int or not 2 <= source_timeout <= 20:
        raise ToolError('invalid_setting_pool_source_timeout_seconds')
    if (not isinstance(mirror, dict) or type(mirror.get('enabled')) is not bool
            or type(mirror.get('max_age_hours')) is not int or not 1 <= mirror['max_age_hours'] <= 168):
        raise ToolError('invalid_setting_mirror_fallback')
    if not isinstance(settings.get('source_urls'), list):
        raise ToolError('invalid_setting_source_urls')
    # Extend the original built-in source for existing installations in memory.
    # Custom-only/empty sources and the saved private file remain unchanged.
    sources = settings['source_urls']
    if OFFICIAL_PRIMARY_URL in sources and OFFICIAL_BACKUP_URL not in sources:
        sources = list(sources)
        sources.insert(sources.index(OFFICIAL_PRIMARY_URL)+1, OFFICIAL_BACKUP_URL)
        settings['source_urls'] = sources
    limits = {"node_timeout_seconds": 120,
              "intelligence_ttl_seconds": 604800, "intelligence_query_limit": 1000}
    # node_timeout_seconds is the画像 HTTP timeout (not per-node probe timeout);
    # kept name for existing pool.json compatibility.
    for key, maximum in limits.items():
        value = settings.get(key)
        if type(value) is not int or not 1 <= value <= maximum:
            raise ToolError("invalid_setting_" + key)
    for url in settings["source_urls"]:
        https_url(url)
    if not all(re.fullmatch(r"[A-Z]{2}", country) for country in settings["countries"]):
        raise ToolError("invalid_country_filter")
    return settings
