import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlsplit

ROUTES = {
    "/version": {"major": "1", "minor": "33", "gitVersion": "v1.33.0", "platform": "linux/arm64"},
    "/api": {"kind": "APIVersions", "versions": ["v1"]},
    "/apis": {"kind": "APIGroupList", "apiVersion": "v1", "groups": []},
    "/api/v1": {"kind": "APIResourceList", "groupVersion": "v1", "resources": []},
}


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        path = urlsplit(self.path).path
        body = ROUTES.get(path)
        code = 200 if body is not None else 404
        payload = json.dumps(body if body is not None else {"kind": "Status", "code": 404}).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)
        sys.stderr.write("GET %s -> %d\n" % (path, code))
        sys.stderr.flush()

    def log_message(self, fmt, *args):
        pass


if __name__ == "__main__":
    HTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
