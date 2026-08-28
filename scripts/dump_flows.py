"""mitmproxy addon: dump every captured message as one JSON object per line.

Usage:
    mitmdump -nr captures/bulb.mitm -s scripts/dump_flows.py > captures/bulb.jsonl

Handles both HTTP flows and raw TCP flows (the `tcp_hosts` mode used for the bulb).
Payloads are emitted as hex plus a printable-ASCII rendering so binary protocols survive
the round trip; `flow_detail` output alone mangles them.
"""

import json
import sys

from mitmproxy import http, tcp


def _printable(data: bytes) -> str:
    return "".join(chr(b) if 32 <= b < 127 else "." for b in data)


def _emit(record: dict) -> None:
    json.dump(record, sys.stdout)
    sys.stdout.write("\n")


def _peer(flow) -> dict:
    client = flow.client_conn
    server = flow.server_conn
    return {
        "client": f"{client.peername[0]}:{client.peername[1]}" if client.peername else None,
        "server": f"{server.peername[0]}:{server.peername[1]}" if server.peername else None,
        "sni": getattr(server, "sni", None),
        "tls": bool(getattr(server, "tls_established", False)),
    }


def tcp_message(flow: tcp.TCPFlow) -> None:
    message = flow.messages[-1]
    data = bytes(message.content)
    _emit(
        {
            "kind": "tcp",
            "flow_id": flow.id,
            "timestamp": message.timestamp,
            "direction": "client->server" if message.from_client else "server->client",
            "length": len(data),
            "hex": data.hex(),
            "ascii": _printable(data),
            **_peer(flow),
        }
    )


def response(flow: http.HTTPFlow) -> None:
    request_body = flow.request.raw_content or b""
    response_body = flow.response.raw_content or b"" if flow.response else b""
    _emit(
        {
            "kind": "http",
            "flow_id": flow.id,
            "timestamp": flow.request.timestamp_start,
            "method": flow.request.method,
            "url": flow.request.pretty_url,
            "request_headers": dict(flow.request.headers),
            "request_hex": request_body.hex(),
            "request_ascii": _printable(request_body),
            "status": flow.response.status_code if flow.response else None,
            "response_headers": dict(flow.response.headers) if flow.response else {},
            "response_hex": response_body.hex(),
            "response_ascii": _printable(response_body),
            **_peer(flow),
        }
    )
