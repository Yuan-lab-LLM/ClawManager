"""Run inside a network=none container against its loopback HTTP service."""
import json
import os
import time
import urllib.error
import urllib.request


BASE = "http://127.0.0.1:8000"


def request(path, payload=None, authenticated=True):
    headers = {"Content-Type": "application/json"}
    if authenticated:
        headers["Authorization"] = "Bearer " + os.environ["LAYA_API_KEY"]
    data = json.dumps(payload, ensure_ascii=False).encode() if payload is not None else None
    try:
        with urllib.request.urlopen(urllib.request.Request(BASE + path, data=data, headers=headers), timeout=120) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as exc:
        return exc.code, json.load(exc)


def main():
    status, health = request("/health", authenticated=False)
    assert status == 200 and health["loaded"] == ["multilingual"], health
    assert health["device"] == "cpu", health
    print(json.dumps({"health": health}, ensure_ascii=False), flush=True)
    payload = {"state": "你好，请帮我写一个 Python 排序函数。", "questions": {
        "category": {"type": "choice", "instructions": "判断用户的问题类别。", "criteria": {
            "chat": "闲聊或问候", "coding": "编写或修改程序代码", "math": "数学证明与推导"}}}}
    assert request("/v1/systemone", payload, authenticated=False)[0] == 401
    for prompt in (payload["state"], "Please write a Python sorting function."):
        payload["state"] = prompt
        start = time.monotonic()
        status, result = request("/v1/systemone", payload)
        assert status == 200 and "category" in result["answers"], result
        assert result["routing"]["model"] == "multilingual", result
        print(json.dumps({"prompt": prompt, "seconds": round(time.monotonic() - start, 3), "result": result}, ensure_ascii=False), flush=True)
    payload["model"] = "english"
    assert request("/v1/systemone", payload)[0] == 422
    payload["model"] = "multilingual"
    payload["max_len"] = 1000000
    assert request("/v1/systemone", payload)[0] == 422
    print("PASS: offline HTTP inference, CPU, Chinese/English, auth and request limits", flush=True)


if __name__ == "__main__":
    main()
