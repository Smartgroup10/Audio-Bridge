#!/usr/bin/env python3
"""
Echo WSS Server - Test tool for Audio Bridge
=============================================
Receives audio from the Bridge via WebSocket and sends it back (echo).
Also simulates AI control events for testing transfers and hangups.

Usage:
    pip install websockets
    python echo_server.py [--port 9093] [--ssl]

With SSL (for WSS):
    python echo_server.py --ssl --cert cert.pem --key key.pem

What it does:
    - Accepts WSS connections from the Bridge
    - Logs all connection parameters (notaria_id, caller_id, etc.)
    - Echoes audio back (you hear yourself with a slight delay)
    - Type commands in the terminal to send control events:
        't' = send transfer event
        'h' = send hangup event
        'p' = send hold event
        'q' = close connection
"""

import asyncio
import json
import ssl
import sys
import argparse
from urllib.parse import urlparse, parse_qs

try:
    import websockets
except ImportError:
    print("Install websockets: pip install websockets")
    sys.exit(1)


class EchoServer:
    def __init__(self, port=9093, use_ssl=False, cert=None, key=None):
        self.port = port
        self.use_ssl = use_ssl
        self.cert = cert
        self.key = key
        self.active_connections = {}
        self.connection_counter = 0

    async def handle_connection(self, websocket):
        """Handle a single WebSocket connection from the Bridge"""
        self.connection_counter += 1
        conn_id = self.connection_counter

        # Parse query parameters (metadata from Bridge)
        path = websocket.request.path if hasattr(websocket, 'request') else ""
        params = {}
        if '?' in str(websocket.path):
            query = str(websocket.path).split('?', 1)[1]
            params = parse_qs(query)

        # Flatten params
        meta = {k: v[0] if len(v) == 1 else v for k, v in params.items()}

        print(f"\n{'='*60}")
        print(f"  NEW CONNECTION #{conn_id}")
        print(f"{'='*60}")
        print(f"  Remote: {websocket.remote_address}")
        for k, v in meta.items():
            print(f"  {k}: {v}")
        print(f"{'='*60}")
        print(f"  Commands: [t]ransfer  [h]angup  [p]ause/hold  [q]uit")
        print(f"{'='*60}\n")

        self.active_connections[conn_id] = websocket
        audio_frames = 0
        total_bytes = 0

        try:
            async for message in websocket:
                if isinstance(message, bytes):
                    # Binary frame = audio
                    audio_frames += 1
                    total_bytes += len(message)

                    if audio_frames % 50 == 0:  # Log every ~1 second (at 20ms frames)
                        print(f"  [#{conn_id}] Audio: {audio_frames} frames, "
                              f"{total_bytes/1024:.1f} KB received")

                    # Echo the audio back
                    try:
                        await websocket.send(message)
                    except:
                        break

                elif isinstance(message, str):
                    # Text frame = JSON event from Bridge
                    try:
                        event = json.loads(message)
                        print(f"  [#{conn_id}] Event received: {json.dumps(event, indent=2)}")
                    except json.JSONDecodeError:
                        print(f"  [#{conn_id}] Text received: {message[:200]}")

        except websockets.exceptions.ConnectionClosed as e:
            print(f"  [#{conn_id}] Connection closed: {e}")
        except Exception as e:
            print(f"  [#{conn_id}] Error: {e}")
        finally:
            del self.active_connections[conn_id]
            duration_sec = audio_frames * 0.02  # 20ms per frame
            print(f"\n  [#{conn_id}] DISCONNECTED")
            print(f"  [#{conn_id}] Total: {audio_frames} frames, "
                  f"{total_bytes/1024:.1f} KB, ~{duration_sec:.1f}s audio")
            print()

    async def send_event(self, conn_id, event):
        """Send a JSON event to a specific connection"""
        ws = self.active_connections.get(conn_id)
        if ws is None:
            print(f"  Connection #{conn_id} not found")
            return
        try:
            data = json.dumps(event)
            await ws.send(data)
            print(f"  [#{conn_id}] Sent: {data}")
        except Exception as e:
            print(f"  [#{conn_id}] Send error: {e}")

    async def command_loop(self):
        """Read terminal commands to send control events"""
        loop = asyncio.get_event_loop()

        while True:
            try:
                cmd = await loop.run_in_executor(None, input, "")
                cmd = cmd.strip().lower()

                if not self.active_connections:
                    if cmd:
                        print("  No active connections")
                    continue

                # Use the latest connection
                conn_id = max(self.active_connections.keys())

                if cmd == 't':
                    await self.send_event(conn_id, {
                        "event": "transfer",
                        "destination": "201",
                        "destination_type": "extension",
                        "notaria_id": "N001",
                        "via": "sip_trunk"
                    })
                elif cmd == 'h':
                    await self.send_event(conn_id, {
                        "event": "hangup",
                        "reason": "resolved"
                    })
                elif cmd == 'p':
                    await self.send_event(conn_id, {
                        "event": "hold",
                        "action": "start",
                        "moh": True
                    })
                elif cmd == 'q':
                    ws = self.active_connections.get(conn_id)
                    if ws:
                        await ws.close()
                        print(f"  [#{conn_id}] Connection closed")
                elif cmd:
                    print("  Unknown command. Use: [t]ransfer [h]angup [p]ause [q]uit")

            except (EOFError, KeyboardInterrupt):
                break
            except Exception as e:
                print(f"  Command error: {e}")

    async def run(self):
        """Start the echo server"""
        ssl_context = None
        if self.use_ssl:
            ssl_context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            ssl_context.load_cert_chain(self.cert, self.key)

        protocol = "wss" if self.use_ssl else "ws"
        print(f"\n  Echo WSS Server starting on {protocol}://0.0.0.0:{self.port}")
        print(f"  Waiting for Bridge connections...\n")

        async with websockets.serve(
            self.handle_connection,
            "0.0.0.0",
            self.port,
            ssl=ssl_context,
            ping_interval=20,
            ping_timeout=60,
        ):
            await self.command_loop()


def main():
    parser = argparse.ArgumentParser(description="Echo WSS Server for Audio Bridge testing")
    parser.add_argument("--port", type=int, default=9093, help="Listen port (default: 9093)")
    parser.add_argument("--ssl", action="store_true", help="Enable SSL/TLS")
    parser.add_argument("--cert", default="cert.pem", help="SSL certificate file")
    parser.add_argument("--key", default="key.pem", help="SSL private key file")
    args = parser.parse_args()

    server = EchoServer(
        port=args.port,
        use_ssl=args.ssl,
        cert=args.cert,
        key=args.key,
    )

    try:
        asyncio.run(server.run())
    except KeyboardInterrupt:
        print("\n  Server stopped.")


if __name__ == "__main__":
    main()
