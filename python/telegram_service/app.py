import os
import json
import logging
import asyncio
from flask import Flask, request, jsonify
from telethon.sync import TelegramClient
from telethon.sessions import StringSession

app = Flask(__name__)

async def get_new_messages(api_id: int, api_hash: str, session_string: str, channel: str, last_id: int):
    client = TelegramClient(StringSession(session_string), api_id, api_hash)
    try:
        await client.connect()
        if not await client.is_user_authorized():
            logging.error("Authorization failed with the provided session string.")
            raise ConnectionError("Telegram authorization failed. Please generate a new session string.")

        messages_data = []
        async for message in client.iter_messages(channel, min_id=last_id, limit=100):
            if message and message.text:
                messages_data.append({
                    "id": message.id,
                    "text": message.text,
                    "date": message.date.isoformat(),
                    "link": f"https://t.me/{channel}/{message.id}",
                })
        return messages_data
    finally:
        if client.is_connected():
            await client.disconnect()

@app.post("/api/v1/telegram/updates")
def telegram_updates():
    try:
        body = request.get_json(force=True)
        channel = body.get("channel")
        last_id = int(body.get("last_id", 0))
    except Exception:
        return ("Invalid JSON body. Expected { 'channel': 'name', 'last_id'?: number }", 400)

    if not channel:
        return ("The 'channel' field is required in the request body.", 400)

    api_id_str = os.environ.get("TELEGRAM_API_ID")
    api_hash = os.environ.get("TELEGRAM_API_HASH")
    # Trim to avoid issues with newline characters in secret values
    session_string = (os.environ.get("TELEGRAM_SESSION_STRING") or "").strip()
    if not all([api_id_str, api_hash, session_string]):
        logging.error("Missing env: TELEGRAM_API_ID/TELEGRAM_API_HASH/TELEGRAM_SESSION_STRING")
        return ("Server configuration is incomplete.", 500)

    try:
        api_id = int(api_id_str)
        logging.info(f"Fetching messages for channel '{channel}' after id {last_id}")
        posts = asyncio.run(get_new_messages(api_id, api_hash, session_string, channel, last_id))
        return jsonify(posts), 200
    except Exception as e:
        logging.exception("Failed to fetch telegram updates")
        return (f"Internal error: {e}", 500)

@app.get("/healthz")
def healthz():
    return jsonify({"ok": True}), 200

@app.get("/")
def root():
    return jsonify({"service": "telegram", "status": "ok"}), 200

if __name__ == "__main__":
    port = int(os.environ.get("PORT", 8080))
    app.run(host="0.0.0.0", port=port)


