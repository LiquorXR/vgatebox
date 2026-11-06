import io
import json
import os
import re
import sys

root = sys.argv[1] if len(sys.argv) > 1 else "."
src_path = os.path.join(root, "vps", "fanout", "routes.go")
out_path = os.path.join(root, "vps", "fanout", "testdata", "api", "routes.json")

with io.open(src_path, encoding="utf-8") as f:
    src = f.read()

pat = re.compile(
    r'\{Path:\s*"([^"]+)",\s*Methods:\s*\[\]string\{([^}]*)\},'
    r'\s*Handler:\s*"([^"]+)",\s*Auth:\s*"([^"]+)"\}'
)
routes = []
for m in pat.finditer(src):
    path, methods, handler, auth = m.groups()
    routes.append(
        {
            "path": path,
            "methods": re.findall(r'"([A-Z]+)"', methods),
            "handler": handler,
            "auth": auth,
        }
    )

print("parsed routes:", len(routes))
os.makedirs(os.path.dirname(out_path), exist_ok=True)
doc = {
    "schema_version": 1,
    "generated_from": "vps/fanout/routes.go allAPIRoutes()",
    "routes": routes,
}
with io.open(out_path, "w", encoding="utf-8", newline="\n") as f:
    f.write(json.dumps(doc, ensure_ascii=False, indent=2) + "\n")
print("wrote", out_path)
