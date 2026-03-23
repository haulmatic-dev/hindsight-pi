#!/usr/bin/env python3
import os
import sys
import time

# Set environment variables before importing hindsight
os.environ["HINDSIGHT_API_LLM_PROVIDER"] = "openai"
os.environ["HINDSIGHT_API_LLM_API_KEY"] = os.environ.get("KIMI_API_KEY", "")
os.environ["HINDSIGHT_API_LLM_BASE_URL"] = "https://api.moonshot.cn/v1"
os.environ["HINDSIGHT_API_LLM_MODEL"] = "kimi-k2-5"
os.environ["HINDSIGHT_API_LOG_LEVEL"] = "info"

print("Starting Hindsight daemon...")
print(f"API Key: {os.environ['HINDSIGHT_API_LLM_API_KEY'][:20]}...")
print(f"Base URL: {os.environ['HINDSIGHT_API_LLM_BASE_URL']}")

from hindsight import HindsightEmbedded

print("Creating HindsightEmbedded client...")
client = HindsightEmbedded(profile="hydra")
print(f"✅ Daemon started at: {client.url}")

# Keep the daemon running
while True:
    try:
        time.sleep(1)
    except KeyboardInterrupt:
        print("\nShutting down...")
        client.close()
        break
