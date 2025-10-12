#!/usr/bin/env python3
"""
Generate a Telegram session string for use with the telegram service.
Usage:
    1. pip install telethon
    2. python generate_session.py
    3. Enter your API credentials and phone number
    4. Copy the generated session string to GCP Secret Manager
"""
import asyncio
from telethon.sync import TelegramClient
from telethon.sessions import StringSession

async def main():
    print("=" * 60)
    print("Telegram Session String Generator")
    print("=" * 60)
    
    api_id = input("Enter TELEGRAM_API_ID: ").strip()
    api_hash = input("Enter TELEGRAM_API_HASH: ").strip()
    
    if not api_id or not api_hash:
        print("Error: API ID and API Hash are required")
        return
    
    try:
        api_id = int(api_id)
    except ValueError:
        print("Error: API ID must be a number")
        return
    
    print("\nConnecting to Telegram...")
    client = TelegramClient(StringSession(), api_id, api_hash)
    
    await client.start()
    
    if await client.is_user_authorized():
        session_string = client.session.save()
        print("\n" + "=" * 60)
        print("✓ Session created successfully!")
        print("=" * 60)
        print("\nYour TELEGRAM_SESSION_STRING:")
        print("-" * 60)
        print(session_string)
        print("-" * 60)
        print("\nTo update the secret in GCP, run:")
        print(f'echo -n "{session_string}" | gcloud secrets versions add TELEGRAM_SESSION_STRING --data-file=-')
        print("=" * 60)
    else:
        print("\n✗ Authorization failed")
    
    await client.disconnect()

if __name__ == "__main__":
    asyncio.run(main())

