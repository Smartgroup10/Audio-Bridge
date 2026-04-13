#!/usr/bin/env python3
"""
Lakimi Echo Server — WSS test server for the multiplexed Lakimi protocol.

Simulates Lakimi's behavior:
- Accepts WSS connections (no mTLS for local testing)
- Receives "start" events and registers sessions
- Echoes back "media" audio frames with a short delay
- Sends periodic "transcript" events (fake)
- Responds to "stop" events
- Supports multiple concurrent sessions on a single WSS

Usage:
    pip install websockets
    python lakimi_echo_server.py [--port 9999]
"""

import argparse
import asyncio
import json
import time
import base64
import logging

try:
    import websockets
except ImportError:
    print("Install websockets: pip install websockets")
    exit(1)

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
log = logging.getLogger("lakimi-echo")

# Active sessions: interaction_id -> metadata
sessions = {}

# Audio echo queue per session
echo_queues = {}


async def handle_connection(websocket):
    """Handle a single WSS connection (may carry multiple sessions)."""
    remote = websocket.remote_address
    log.info(f"New connection from {remote}")

    try:
        async for raw_msg in websocket:
            try:
                msg = json.loads(raw_msg)
            except json.JSONDecodeError:
                log.warning(f"Invalid JSON from {remote}: {raw_msg[:100]}")
                continue

            event = msg.get("event", "")
            interaction_id = msg.get("interaction_id", "")

            if event == "start":
                sessions[interaction_id] = {
                    "notaria_id": msg.get("notaria_id", ""),
                    "caller_id": msg.get("caller_id", ""),
                    "started_at": time.time(),
                }
                echo_queues[interaction_id] = asyncio.Queue()
                log.info(f"Session started: {interaction_id} "
                         f"(notaria={msg.get('notaria_id')}, caller={msg.get('caller_id')})")

                # Send a greeting transcript after a short delay
                asyncio.create_task(
                    send_greeting(websocket, interaction_id)
                )
                # Start echo loop for this session
                asyncio.create_task(
                    echo_loop(websocket, interaction_id)
                )

            elif event == "media":
                if interaction_id in echo_queues:
                    await echo_queues[interaction_id].put(msg.get("payload", ""))
                else:
                    log.warning(f"Media for unknown session: {interaction_id}")

            elif event == "stop":
                duration = 0
                if interaction_id in sessions:
                    duration = time.time() - sessions[interaction_id]["started_at"]
                    del sessions[interaction_id]
                if interaction_id in echo_queues:
                    await echo_queues[interaction_id].put(None)  # Signal echo loop to stop
                    del echo_queues[interaction_id]
                log.info(f"Session stopped: {interaction_id} "
                         f"(reason={msg.get('reason')}, duration={duration:.1f}s)")

            elif event == "dtmf":
                log.info(f"DTMF received: {interaction_id} digit={msg.get('digit')}")
                # Echo back a transfer on digit "0"
                if msg.get("digit") == "0":
                    transfer_msg = {
                        "event": "transfer",
                        "interaction_id": interaction_id,
                        "destination": "200",
                        "destination_type": "extension",
                        "reason": "dtmf_transfer_request",
                    }
                    await websocket.send(json.dumps(transfer_msg))
                    log.info(f"Sent transfer event for {interaction_id}")

            elif event == "transfer_completed":
                log.info(f"Transfer result: {interaction_id} "
                         f"dest={msg.get('destination')} status={msg.get('status')}")

            else:
                log.info(f"Unknown event '{event}' from {interaction_id}")

    except websockets.exceptions.ConnectionClosed:
        log.info(f"Connection closed from {remote}")
    finally:
        # Clean up all sessions on this connection
        for sid in list(sessions.keys()):
            if sid in echo_queues:
                await echo_queues[sid].put(None)
                del echo_queues[sid]
            del sessions[sid]
            log.info(f"Cleaned up session {sid} (connection lost)")


async def send_greeting(websocket, interaction_id):
    """Send a fake AI transcript after a brief delay."""
    await asyncio.sleep(0.5)
    if interaction_id not in sessions:
        return

    greeting = {
        "event": "transcript",
        "interaction_id": interaction_id,
        "transcript": "Hola, buenos dias. En que puedo ayudarle?",
        "role": "ai",
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    try:
        await websocket.send(json.dumps(greeting))
        log.info(f"Sent AI greeting transcript for {interaction_id}")
    except websockets.exceptions.ConnectionClosed:
        pass


async def echo_loop(websocket, interaction_id):
    """Echo back audio with a 100ms delay to simulate AI processing."""
    queue = echo_queues.get(interaction_id)
    if queue is None:
        return

    while True:
        payload = await queue.get()
        if payload is None:
            break

        # Small delay to simulate processing
        await asyncio.sleep(0.1)

        if interaction_id not in sessions:
            break

        echo_msg = {
            "event": "media",
            "interaction_id": interaction_id,
            "payload": payload,  # Echo same audio back
        }
        try:
            await websocket.send(json.dumps(echo_msg))
        except websockets.exceptions.ConnectionClosed:
            break


async def main(host, port):
    log.info(f"Lakimi Echo Server starting on ws://{host}:{port}")
    log.info("Sessions will echo audio back with 100ms delay")
    log.info("Send DTMF '0' to trigger a transfer event")

    async with websockets.serve(handle_connection, host, port):
        await asyncio.Future()  # Run forever


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Lakimi Echo Server for testing")
    parser.add_argument("--host", default="0.0.0.0", help="Bind host (default: 0.0.0.0)")
    parser.add_argument("--port", type=int, default=9999, help="Bind port (default: 9999)")
    args = parser.parse_args()

    asyncio.run(main(args.host, args.port))
