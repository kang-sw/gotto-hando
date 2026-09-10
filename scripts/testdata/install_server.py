"""Local release fixture, including a deliberately interrupted binary response."""
import functools
import http.server
import pathlib
import sys

root, port_file = map(pathlib.Path, sys.argv[1:])


class Handler(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        control = root / 'truncate'
        path = pathlib.Path(self.translate_path(self.path))
        if control.exists() and path.name == control.read_text().strip() and path.is_file():
            data = path.read_bytes()
            self.send_response(200)
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data[:max(1, len(data) // 2)])
            self.close_connection = True
            return
        super().do_GET()

    def guess_type(self, path):
        if pathlib.Path(path).name == 'latest':
            return 'application/json'
        return super().guess_type(path)


with http.server.ThreadingHTTPServer(('127.0.0.1', 0), functools.partial(Handler, directory=str(root))) as server:
    port_file.write_text(str(server.server_port))
    server.serve_forever()
