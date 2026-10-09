"""显式 opt-in 的 Gateway 流式验证；不输出 JWT、Prompt 或完整回答。"""

import argparse
import json
from pathlib import Path
from urllib.parse import urlsplit

import httpx


def main() -> int:
    parser = argparse.ArgumentParser(description="已配置模型的 Gateway 流式 smoke")
    parser.add_argument("--gateway-url", required=True)
    parser.add_argument("--access-token-file", required=True, type=Path)
    parser.add_argument("--agent-key", default="learning_assistant")
    parser.add_argument("--message", required=True)
    args = parser.parse_args()
    try:
        address = urlsplit(args.gateway_url)
        if address.scheme not in {"https", "http"} or address.username or address.password:
            raise ValueError
        if args.access_token_file.stat().st_mode & 0o077:
            raise ValueError
        token = args.access_token_file.read_text().strip()
        chunks = 0
        run_id = None
        with httpx.Client(trust_env=False, follow_redirects=False, timeout=60) as client:
            with client.stream(
                "POST",
                args.gateway_url.rstrip("/") + "/api/v1/agent/chat",
                headers={"Authorization": "Bearer " + token},
                json={"message": args.message, "agent_key": args.agent_key},
            ) as response:
                if response.status_code != 200:
                    print(json.dumps({"status": "failed", "http_status": response.status_code}))
                    return 1
                for line in response.iter_lines():
                    if not line.startswith("data: "):
                        continue
                    event = json.loads(line[6:])
                    run_id = event.get("run_id")
                    if event.get("type") == "token":
                        chunks += 1
                    elif event.get("type") == "error":
                        print(json.dumps({"status": "failed", "code": event["data"]["code"]}))
                        return 1
                    elif event.get("type") == "done" and chunks:
                        print(
                            json.dumps(
                                {"status": "completed", "run_id": run_id, "text_chunks": chunks}
                            )
                        )
                        return 0
        print(json.dumps({"status": "incomplete"}))
    except Exception:
        print(json.dumps({"status": "failed", "code": "MODEL_SMOKE_FAILED"}))
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
