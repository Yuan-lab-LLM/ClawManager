"""Resolve public GHCR latest to a date release tag with identical index digest."""
import datetime
import http.client
import json
import re
import time
import urllib.error
import urllib.parse
import urllib.request

ACCEPT = ", ".join((
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
    "application/vnd.oci.image.manifest.v1+json",
    "application/vnd.docker.distribution.manifest.v2+json",
))


class Registry:
    def __init__(self, repository):
        self.repository = repository
        self.token = None

    def request(self, suffix, method="GET"):
        for attempt in range(3):
            try:
                return self._request(suffix, method)
            except (urllib.error.URLError, http.client.HTTPException, TimeoutError) as error:
                if attempt == 2:
                    raise ValueError(f"GHCR network request failed for {self.repository}: {type(error).__name__}") from None
                time.sleep(attempt + 1)

    def _request(self, suffix, method="GET"):
        url = f"https://ghcr.io/v2/{self.repository}/{suffix}"
        headers = {"Accept": ACCEPT}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        try:
            return urllib.request.urlopen(urllib.request.Request(url, headers=headers, method=method), timeout=30)
        except urllib.error.HTTPError as error:
            if error.code != 401 or self.token:
                raise ValueError(f"GHCR metadata request failed ({error.code}) for {self.repository}") from None
            query = urllib.parse.urlencode({"service": "ghcr.io", "scope": f"repository:{self.repository}:pull"})
            try:
                with urllib.request.urlopen("https://ghcr.io/token?" + query, timeout=30) as response:
                    self.token = json.load(response)["token"]
            except (urllib.error.URLError, KeyError) as exc:
                raise ValueError("Cannot resolve public GHCR release metadata; check network/package visibility") from exc
            return self.request(suffix, method)

    def digest(self, tag):
        with self.request("manifests/" + urllib.parse.quote(tag, safe=""), "HEAD") as response:
            digest = response.headers.get("Docker-Content-Digest", "")
        if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
            raise ValueError("GHCR returned no valid manifest digest")
        return digest

    def tags(self):
        suffix = "tags/list?n=1000"
        seen = set()
        while suffix:
            if suffix in seen:
                raise ValueError("GHCR tag pagination loop")
            seen.add(suffix)
            with self.request(suffix) as response:
                payload = json.load(response)
                link = response.headers.get("Link", "")
            yield from payload.get("tags", []) or []
            match = re.search(r'<([^>]+)>;\s*rel="?next"?', link)
            if not match:
                break
            parsed = urllib.parse.urlsplit(urllib.parse.urljoin(
                f"https://ghcr.io/v2/{self.repository}/{suffix}", match.group(1)))
            prefix = f"/v2/{self.repository}/"
            if parsed.scheme != "https" or parsed.netloc != "ghcr.io" or not parsed.path.startswith(prefix):
                raise ValueError("Unexpected GHCR pagination URL")
            suffix = parsed.path[len(prefix):] + ("?" + parsed.query if parsed.query else "")


def date_version(tag):
    match = re.fullmatch(r"v?(\d{4})\.(\d{1,2})\.(\d{1,2})(?:[.-](\d+))?", tag)
    if not match:
        return None
    year, month, day, revision = (int(value or 0) for value in match.groups())
    try:
        datetime.date(year, month, day)
    except ValueError:
        return None
    return year, month, day, revision, tag


def resolve_latest(image):
    if not image.startswith("ghcr.io/") or not image.endswith(":latest"):
        raise ValueError("latest resolution supports public ghcr.io images only")
    repository = image[len("ghcr.io/"):].rsplit(":", 1)[0]
    registry = Registry(repository)
    digest = registry.digest("latest")
    candidates = sorted((version for tag in registry.tags()
                         if (version := date_version(tag))), reverse=True)
    for *_, tag in candidates:
        if registry.digest(tag) == digest:
            return f"ghcr.io/{repository}:{tag}", digest
    raise ValueError(f"{image}: no date release tag has the same digest as latest; specify an explicit tag")
