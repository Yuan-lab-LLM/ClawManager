// desktop_auth.js
//
// njs helper used by the in-pod nginx to decide where a desktop proxy request
// should be sent. It locally verifies the ClawManager instance-access JWT
// (HS256, same secret as the Go control plane) and returns a proxy_pass target:
//
//   "deny"                     -> token missing/invalid/expired/mismatched
//   "https://<host:port>"      -> direct connection to the instance Service
//                                 (taken from the token's "upstream" claim)
//   "http://127.0.0.1:9001"    -> fall back to the in-process control-plane
//                                 proxy (gray rollout / legacy tokens without
//                                 an "upstream" claim)
//
// The function runs in the main request context (js_set), so query args and
// cookies of the original request are available.

var crypto = require('crypto');

var CONTROL_PLANE_FALLBACK = 'http://127.0.0.1:9001';
var DENY = 'deny';

function secret() {
    return process.env.INSTANCE_ACCESS_TOKEN_SECRET || process.env.JWT_SECRET || '';
}

// Normalize base64url / base64 to pad-less standard base64 for comparison.
function toStdB64NoPad(s) {
    return String(s).replace(/-/g, '+').replace(/_/g, '/').replace(/=+$/, '');
}

function b64urlToString(s) {
    var std = String(s).replace(/-/g, '+').replace(/_/g, '/');
    while (std.length % 4 !== 0) {
        std += '=';
    }
    return Buffer.from(std, 'base64').toString();
}

function readCookieToken(r) {
    var cookie = r.headersIn['Cookie'];
    if (!cookie) {
        return '';
    }

    var name = 'instance_access_' + r.variables.inst_id;
    var parts = cookie.split(';');
    for (var i = 0; i < parts.length; i++) {
        var kv = parts[i].trim();
        var eq = kv.indexOf('=');
        if (eq > 0 && kv.substring(0, eq) === name) {
            return kv.substring(eq + 1);
        }
    }
    return '';
}

function readQueryToken(r) {
    if (r.args && r.args.token) {
        return r.args.token;
    }
    return '';
}

function readToken(r) {
    var cookieToken = readCookieToken(r);
    if (cookieToken) {
        return cookieToken;
    }
    return readQueryToken(r);
}

function resolveTarget(r) {
    var key = secret();
    if (!key) {
        r.error('desktop_auth: missing JWT secret in environment');
        return DENY;
    }

    var token = readToken(r);
    if (!token) {
        return DENY;
    }

    var segments = token.split('.');
    if (segments.length !== 3) {
        return DENY;
    }

    var signingInput = segments[0] + '.' + segments[1];
    var expected = crypto.createHmac('sha256', key).update(signingInput).digest('base64');
    if (toStdB64NoPad(expected) !== toStdB64NoPad(segments[2])) {
        return DENY;
    }

    var payload;
    try {
        payload = JSON.parse(b64urlToString(segments[1]));
    } catch (e) {
        return DENY;
    }

    if (payload.token_type !== 'instance_access') {
        return DENY;
    }

    if (payload.exp && (Date.now() / 1000) >= Number(payload.exp)) {
        return DENY;
    }

    if (String(payload.instance_id) !== String(r.variables.inst_id)) {
        return DENY;
    }

    if (payload.upstream) {
        return 'https://' + payload.upstream;
    }

    return CONTROL_PLANE_FALLBACK;
}

// cleanUri returns the original request URI (path + query) with only the
// ClawManager instance-access JWT stripped. Runtime apps such as Hermes may use
// their own "token" query parameter for websocket/session auth; when the
// ClawManager token came from the HttpOnly cookie, that runtime token must be
// preserved and forwarded to the upstream gateway.
function cleanUri(r) {
    var uri = r.variables.request_uri || r.uri || '/';
    var q = uri.indexOf('?');
    if (q < 0) {
        return uri;
    }

    var queryToken = readQueryToken(r);
    if (!queryToken) {
        return uri;
    }
    var cookieToken = readCookieToken(r);
    if (cookieToken && cookieToken !== queryToken) {
        return uri;
    }

    var path = uri.substring(0, q);
    var query = uri.substring(q + 1);
    var parts = query.split('&');
    var kept = [];
    for (var i = 0; i < parts.length; i++) {
        if (parts[i] === '') {
            continue;
        }
        var eq = parts[i].indexOf('=');
        var name = eq < 0 ? parts[i] : parts[i].substring(0, eq);
        var value = eq < 0 ? '' : parts[i].substring(eq + 1);
        if (name === 'token' && queryValueEquals(value, queryToken)) {
            continue;
        }
        kept.push(parts[i]);
    }

    if (kept.length === 0) {
        return path;
    }
    return path + '?' + kept.join('&');
}

function queryValueEquals(rawValue, expected) {
    if (rawValue === expected) {
        return true;
    }
    try {
        return decodeURIComponent(rawValue.replace(/\+/g, ' ')) === expected;
    } catch (e) {
        return false;
    }
}

export default { resolveTarget, cleanUri };
