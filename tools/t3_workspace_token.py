"""Provision the private T3 MCP token for the MILE development workspace."""

import argparse
import base64
import getpass
import hashlib
import json
import os
import secrets
import tempfile
from pathlib import Path
from urllib.error import HTTPError
from urllib.parse import parse_qs, urlencode, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def origin_from_endpoint(endpoint):
    parsed = urlsplit(endpoint)
    loopback = parsed.hostname in {"127.0.0.1", "::1"}
    if (parsed.scheme != "https" and not (parsed.scheme == "http" and loopback)
            or not parsed.hostname or parsed.username is not None
            or parsed.path != "/mcp" or parsed.query or parsed.fragment):
        raise ValueError("Expected an HTTPS /mcp endpoint or a loopback HTTP /mcp endpoint")
    return f"{parsed.scheme}://{parsed.netloc}"


def post(opener, origin, path, payload, *, form=False):
    body = urlencode(payload) if form else json.dumps(payload)
    content_type = "application/x-www-form-urlencoded" if form else "application/json"
    request = Request(origin + path, body.encode(), {"Content-Type": content_type})
    with opener.open(request, timeout=15) as response:
        raw = response.read(64 * 1024 + 1)
    if len(raw) > 64 * 1024:
        raise ValueError("Unexpected response size from T3")
    return json.loads(raw)


def provision(endpoint, pairing_code):
    origin = origin_from_endpoint(endpoint)
    opener = build_opener(NoRedirect())
    callback = "http://127.0.0.1:41801/callback"
    app = post(opener, origin, "/oauth/mcp/register", {
        "client_name": "MILE Development Workspace",
        "redirect_uris": [callback],
        "grant_types": ["authorization_code"],
        "token_endpoint_auth_method": "none",
    })
    verifier = secrets.token_urlsafe(48)
    challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).decode().rstrip("=")
    state = secrets.token_urlsafe(24)
    authorization = {
        "response_type": "code", "client_id": app["client_id"], "redirect_uri": callback,
        "code_challenge": challenge, "code_challenge_method": "S256", "resource": endpoint, "state": state,
    }
    decision = post(opener, origin, "/oauth/mcp/decision", {
        "authorization": authorization,
        "decision": {"_tag": "pairing-code", "code": pairing_code, "access": "full-access"},
    })
    redirect = urlsplit(decision["redirectTo"])
    if redirect._replace(query="").geturl() != callback:
        raise ValueError("Unexpected T3 OAuth callback")
    params = parse_qs(redirect.query)
    if params.get("state") != [state] or len(params.get("code", [])) != 1:
        raise ValueError("T3 authorization failed")
    result = post(opener, origin, "/oauth/mcp/token", {
        "grant_type": "authorization_code", "code": params["code"][0], "client_id": app["client_id"],
        "redirect_uri": callback, "code_verifier": verifier, "resource": endpoint,
    }, form=True)
    token = result.get("access_token")
    ttl = result.get("expires_in")
    if not isinstance(token, str) or not token or any(char.isspace() for char in token):
        raise ValueError("T3 did not issue an access token")
    if not isinstance(ttl, int) or ttl <= 0:
        raise ValueError("T3 did not report a token lifetime")
    return token, ttl


def write_token(path, token):
    # NamedTemporaryFile uses private permissions on Unix. Keep the parent private on Windows.
    with tempfile.NamedTemporaryFile(mode="w", encoding="ascii", dir=path.parent, delete=False) as file:
        temporary = Path(file.name)
        try:
            file.write(token + "\n")
        except OSError:
            file.close()
            temporary.unlink(missing_ok=True)
            raise
    try:
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", required=True, help="Private T3 /mcp endpoint")
    parser.add_argument("--output", required=True, type=Path, help="Secret file read by the MILE backend")
    args = parser.parse_args()
    try:
        origin_from_endpoint(args.endpoint)
    except ValueError as error:
        parser.exit(1, f"{error}\n")
    pairing_code = getpass.getpass("T3 pairing code for Full access: ").strip()
    if not pairing_code:
        parser.exit(1, "A pairing code is required.\n")
    try:
        token, ttl = provision(args.endpoint, pairing_code)
        write_token(args.output, token)
    except HTTPError as error:
        parser.exit(1, f"T3 token provisioning returned HTTP {error.code}\n")
    except (OSError, ValueError, KeyError):
        parser.exit(1, "T3 token provisioning failed. Check the endpoint, pairing code and output directory.\n")
    print(f"T3 token saved. Lifetime: {ttl // 86400} days.")


if __name__ == "__main__":
    main()
