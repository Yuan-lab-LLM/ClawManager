#!/usr/bin/env node
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';

const target = process.argv[2] ?? '/usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js';
const patchScript = process.argv[3] ?? path.join(path.dirname(new URL(import.meta.url).pathname), 'patch-openclaw-trusted-proxy-contract.mjs');

function assert(condition, message) {
	if (!condition) {
		throw new Error(message);
	}
}

const completeMissingDeviceCallPattern = /\n([ \t]+)trustedProxyAuthOk,\n\1sharedAuthOk,\n\1authOk,\n\1authMethod,\n\1forwardedPrefix: upgradeReq\.headers\?\.\["x-forwarded-prefix"\],\n\1hasSharedAuth,/;

function readTargetAfterPatchProof() {
	const original = fs.readFileSync(target, 'utf8');
	if (original.includes('function isGtManagerMediatedControlUiAuth(params)') &&
		original.includes('function logGtManagerControlUiAuthDiagnostic(params, decision)') &&
		original.includes('params.sharedAuthOk === true') &&
		completeMissingDeviceCallPattern.test(original)) {
		return original;
	}

	const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'gtclaw-trusted-proxy-'));
	const tempTarget = path.join(tempDir, 'server.impl.js');
	fs.writeFileSync(tempTarget, original);
	const result = spawnSync(process.execPath, [patchScript, tempTarget], { encoding: 'utf8' });
	assert(result.status === 0, `patch script exited ${result.status}: ${result.stderr || result.stdout}`);
	return fs.readFileSync(tempTarget, 'utf8');
}

function modelGtManagerMediatedControlUiAuth(params) {
	const rawPrefix = Array.isArray(params.forwardedPrefix) ? params.forwardedPrefix[0] : params.forwardedPrefix;
	const forwardedPrefix = typeof rawPrefix === 'string' ? rawPrefix.trim() : '';
	const usesSharedSecretAuth = params.authMethod === 'token' || params.authMethod === 'password';
	const mediatedSharedAuthOk = params.sharedAuthOk === true || (params.authOk === true && usesSharedSecretAuth);
	return params.isControlUi === true &&
		params.role === 'operator' &&
		mediatedSharedAuthOk &&
		usesSharedSecretAuth &&
			/^\/api\/v1\/instances\/[0-9]+\/control-ui\/?$/.test(forwardedPrefix);
}

function modelNormalizeGtManagerMediatedControlUiScopes(scopes) {
	const allowed = new Set(['operator.read', 'operator.pairing']);
	const out = new Set(['operator.read', 'operator.pairing']);
	for (const scope of Array.isArray(scopes) ? scopes : []) {
		if (allowed.has(scope)) out.add(scope);
	}
	return [...out].sort();
}

function modelShouldClearUnboundScopes(params) {
	if (params.decision?.gtManagerMediatedControlUi === true) return false;
	return params.decision.kind !== 'allow' || !params.controlUiAuthPolicy.allowBypass && !params.preserveInsecureLocalControlUiScopes && (params.authMethod === 'token' || params.authMethod === 'password' || params.authMethod === 'trusted-proxy' || params.trustedProxyAuthOk === true);
}

const source = readTargetAfterPatchProof();

function extractGtClawHelperBlock(sourceText) {
	const helperBlockPattern = /function gtManagerControlUiAuthDiagnosticsEnabled\(\) \{[\s\S]*?\n\}\n(?=function shouldClearUnboundScopesForMissingDeviceIdentity\(params\) \{)/;
	const match = sourceText.match(helperBlockPattern);
	assert(match, 'GTClaw helper block not found for scoped unsafe params logging scan');
	return match[0];
}

for (const required of [
	'function isGtManagerMediatedControlUiAuth(params)',
	'function evaluateGtManagerMediatedControlUiAuth(params)',
	'function logGtManagerControlUiAuthDiagnostic(params, decision)',
	'GTCLAW_CONTROLUI_AUTH_DIAGNOSTICS',
	'forwardedPrefix: upgradeReq.headers?.["x-forwarded-prefix"]',
	'if (isGtManagerMediatedControlUiAuth(params)) return { kind: "allow", gtManagerMediatedControlUi: true };',
	'function normalizeGtManagerMediatedControlUiScopes(scopes)',
	'params.decision?.gtManagerMediatedControlUi === true',
	'connectParams.scopes = scopes',
	'params.authMethod === "token" || params.authMethod === "password"',
	'params.sharedAuthOk === true',
	'device signature invalid',
	'verifyDeviceSignature',
	'bootstrapTokenCandidate',
	'verifyDeviceToken',
	'resolveConnectAuthDecision',
	'shouldSkipControlUiPairing'
]) {
	assert(source.includes(required), `required runtime contract string missing: ${required}`);
}

assert(completeMissingDeviceCallPattern.test(source), 'evaluateMissingDeviceIdentity call must pass authMethod and x-forwarded-prefix into the same missing-device decision params');

for (const forbidden of [
	'params.trustedProxyMarker',
	'connectParams.trustedProxy',
	'connectParams.auth.trustedProxy',
	'trustedProxyMarker:'
]) {
	assert(!source.includes(forbidden), `runtime contract must not trust unsafe marker string globally: ${forbidden}`);
}

const gtClawHelperBlock = extractGtClawHelperBlock(source);
const normalizeScopeBlockMatch = gtClawHelperBlock.match(/function normalizeGtManagerMediatedControlUiScopes\(scopes\) \{[\s\S]*?\n\}/);
assert(normalizeScopeBlockMatch, 'GTClaw mediated scope normalizer not found');
const normalizeScopeBlock = normalizeScopeBlockMatch[0];
for (const requiredScope of [
	'"operator.read"',
	'"operator.pairing"'
]) {
	assert(normalizeScopeBlock.includes(requiredScope), `mediated scope normalizer missing required scope: ${requiredScope}`);
}
for (const forbiddenScope of [
	'"operator.admin"',
	'"operator.write"',
	'"operator.approvals"',
	'"operator.talk.secrets"'
]) {
	assert(!normalizeScopeBlock.includes(forbiddenScope), `mediated scope normalizer must not grant broad scope: ${forbiddenScope}`);
}
for (const forbidden of [
	'console.error(params)',
	'console.log(params)',
	'JSON.stringify(params)'
]) {
	assert(!gtClawHelperBlock.includes(forbidden), `GTClaw diagnostic helper must not log full params: ${forbidden}`);
}

const cases = [
	['direct JSON marker without auth is rejected', false, { isControlUi: true, role: 'operator', authOk: false, authMethod: undefined, forwardedPrefix: '/api/v1/instances/16/control-ui', trustedProxyMarker: true }],
	['shared token without backend route prefix is rejected', false, { isControlUi: true, role: 'operator', authOk: true, sharedAuthOk: true, authMethod: 'token', forwardedPrefix: '' }],
	['shared token with wrong route prefix is rejected', false, { isControlUi: true, role: 'operator', authOk: true, sharedAuthOk: true, authMethod: 'token', forwardedPrefix: '/control-ui' }],
	['shared auth with missing authMethod is rejected', false, { isControlUi: true, role: 'operator', authOk: true, sharedAuthOk: true, forwardedPrefix: '/api/v1/instances/16/control-ui' }],
	['non-operator with backend route prefix is rejected', false, { isControlUi: true, role: 'viewer', authOk: true, sharedAuthOk: true, authMethod: 'token', forwardedPrefix: '/api/v1/instances/16/control-ui' }],
	['non-control-ui client with backend route prefix is rejected', false, { isControlUi: false, role: 'operator', authOk: true, sharedAuthOk: true, authMethod: 'token', forwardedPrefix: '/api/v1/instances/16/control-ui' }],
	['authOk without shared-secret auth method is rejected', false, { isControlUi: true, role: 'operator', authOk: true, sharedAuthOk: false, authMethod: 'device-token', forwardedPrefix: '/api/v1/instances/16/control-ui' }],
	['authOk with shared-secret authMethod remains allowed for prior contract compatibility', true, { isControlUi: true, role: 'operator', authOk: true, sharedAuthOk: false, authMethod: 'token', forwardedPrefix: '/api/v1/instances/16/control-ui' }],
	['backend-mediated token shared auth is allowed even when authOk path is not the shared proof', true, { isControlUi: true, role: 'operator', authOk: false, sharedAuthOk: true, authMethod: 'token', forwardedPrefix: '/api/v1/instances/16/control-ui' }],
	['backend-mediated password shared auth is allowed even when authOk path is not the shared proof', true, { isControlUi: true, role: 'operator', authOk: false, sharedAuthOk: true, authMethod: 'password', forwardedPrefix: ['/api/v1/instances/16/control-ui/'] }]
];

for (const [name, expected, params] of cases) {
	const actual = modelGtManagerMediatedControlUiAuth(params);
	assert(actual === expected, `${name}: got ${actual}, want ${expected}`);
}

const normalizedActualControlUiScopes = modelNormalizeGtManagerMediatedControlUiScopes([
	'operator.admin',
	'operator.read',
	'operator.write',
	'operator.approvals',
	'operator.pairing'
]);
assert(JSON.stringify(normalizedActualControlUiScopes) === JSON.stringify(['operator.pairing', 'operator.read']), `mediated scope normalization kept unsafe or missing scopes: ${normalizedActualControlUiScopes.join(',')}`);
assert(JSON.stringify(modelNormalizeGtManagerMediatedControlUiScopes([])) === JSON.stringify(['operator.pairing', 'operator.read']), 'mediated scope normalization must grant the minimum Control UI operator scopes when the client scope list is empty');
assert(modelShouldClearUnboundScopes({
	decision: { kind: 'allow', gtManagerMediatedControlUi: true },
	controlUiAuthPolicy: { allowBypass: false },
	preserveInsecureLocalControlUiScopes: false,
	authMethod: 'token',
	trustedProxyAuthOk: false
}) === false, 'GTManager mediated allow must not be followed by unbound-scope clearing');
assert(modelShouldClearUnboundScopes({
	decision: { kind: 'allow' },
	controlUiAuthPolicy: { allowBypass: false },
	preserveInsecureLocalControlUiScopes: false,
	authMethod: 'token',
	trustedProxyAuthOk: false
}) === true, 'non-mediated token allow remains subject to existing unbound-scope clearing');

console.log(`trusted-proxy auth contract verifier passed for ${target}`);
