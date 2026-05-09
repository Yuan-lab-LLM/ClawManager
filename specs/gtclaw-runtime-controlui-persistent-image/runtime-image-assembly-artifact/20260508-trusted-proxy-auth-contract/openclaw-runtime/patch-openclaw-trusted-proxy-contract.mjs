#!/usr/bin/env node
import fs from 'node:fs';

const target = process.argv[2] ?? '/usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js';

let source = fs.readFileSync(target, 'utf8');

const helperBlock = String.raw`function gtManagerControlUiAuthDiagnosticsEnabled() {
	const value = typeof process !== "undefined" ? process.env?.GTCLAW_CONTROLUI_AUTH_DIAGNOSTICS : undefined;
	return value === "1" || value === "true" || value === "yes" || value === "on";
}
function resolveGtManagerForwardedPrefix(value) {
	const raw = Array.isArray(value) ? value[0] : value;
	return normalizeOptionalString(raw) ?? "";
}
function gtManagerControlUiRoleShape(value) {
	if (value === "operator") return "operator";
	if (typeof value === "string" && value.length > 0) return "other";
	return "missing";
}
function gtManagerControlUiAuthMethodShape(value) {
	if (value === "token" || value === "password" || value === "device-token") return value;
	if (typeof value === "string" && value.length > 0) return "other";
	return "missing";
}
function gtManagerControlUiForwardedPrefixShape(forwardedPrefix) {
	if (forwardedPrefix === "") return "missing";
	if (/^\/api\/v1\/instances\/[0-9]+\/control-ui\/?$/.test(forwardedPrefix)) return "backend_control_ui_prefix_match";
	return "wrong_prefix";
}
function evaluateGtManagerMediatedControlUiAuth(params) {
	const forwardedPrefix = resolveGtManagerForwardedPrefix(params.forwardedPrefix);
	const usesSharedSecretAuth = params.authMethod === "token" || params.authMethod === "password";
	const mediatedSharedAuthOk = params.sharedAuthOk === true || (params.authOk === true && usesSharedSecretAuth);
	const forwardedPrefixMatch = /^\/api\/v1\/instances\/[0-9]+\/control-ui\/?$/.test(forwardedPrefix);
	return {
		allow: params.isControlUi === true && params.role === "operator" && mediatedSharedAuthOk && usesSharedSecretAuth && forwardedPrefixMatch,
		condIsControlUi: params.isControlUi === true,
		condRoleOperator: params.role === "operator",
		condSharedAuthProof: mediatedSharedAuthOk,
		condAuthMethodSharedSecret: usesSharedSecretAuth,
		condForwardedPrefixMatch: forwardedPrefixMatch,
		forwardedPrefixShape: gtManagerControlUiForwardedPrefixShape(forwardedPrefix)
		};
	}
function normalizeGtManagerMediatedControlUiScopes(scopes) {
	const allowed = new Set(["operator.read", "operator.pairing"]);
	const out = new Set(["operator.read", "operator.pairing"]);
	if (Array.isArray(scopes)) {
		for (const scope of scopes) {
			const value = typeof scope === "string" ? scope.trim() : "";
			if (allowed.has(value)) out.add(value);
		}
	}
	return Array.from(out).sort();
}
	function logGtManagerControlUiAuthDiagnostic(params, decision) {
		if (!gtManagerControlUiAuthDiagnosticsEnabled()) return;
		const fields = [
		"event=missing_device_decision",
		"is_control_ui=" + (params.isControlUi === true),
		"role=" + gtManagerControlUiRoleShape(params.role),
		"auth_method=" + gtManagerControlUiAuthMethodShape(params.authMethod),
		"auth_ok=" + (params.authOk === true),
		"shared_auth_ok=" + (params.sharedAuthOk === true),
		"has_shared_auth=" + (params.hasSharedAuth === true),
		"trusted_proxy_auth_ok=" + (params.trustedProxyAuthOk === true),
		"control_ui_allow_bypass=" + (params.controlUiAuthPolicy?.allowBypass === true),
		"has_device=" + (params.hasDevice === true),
		"forwarded_prefix_shape=" + decision.forwardedPrefixShape,
		"cond_is_control_ui=" + decision.condIsControlUi,
		"cond_role_operator=" + decision.condRoleOperator,
		"cond_shared_auth_proof=" + decision.condSharedAuthProof,
		"cond_auth_method_shared_secret=" + decision.condAuthMethodSharedSecret,
		"cond_forwarded_prefix_match=" + decision.condForwardedPrefixMatch,
		"mediated_helper_result=" + decision.allow
	];
	console.error("[gtclaw-controlui-auth-diagnostic] " + fields.join(" "));
}
function isGtManagerMediatedControlUiAuth(params) {
	const decision = evaluateGtManagerMediatedControlUiAuth(params);
	logGtManagerControlUiAuthDiagnostic(params, decision);
	return decision.allow;
}
`;

const helperAnchor = 'function shouldClearUnboundScopesForMissingDeviceIdentity(params) {';
if (!source.includes(helperAnchor)) {
	throw new Error(`helper insertion anchor not found in ${target}`);
}

if (!source.includes('function isGtManagerMediatedControlUiAuth(params)')) {
	source = source.replace(helperAnchor, `${helperBlock}${helperAnchor}`);
} else if (!source.includes('function logGtManagerControlUiAuthDiagnostic(params, decision)') || !source.includes('function evaluateGtManagerMediatedControlUiAuth(params)') || !source.includes('function normalizeGtManagerMediatedControlUiScopes(scopes)') || !source.includes('params.sharedAuthOk === true')) {
	const helperSectionPattern = /function (?:gtManagerControlUiAuthDiagnosticsEnabled|resolveGtManagerForwardedPrefix)\([^]*?function isGtManagerMediatedControlUiAuth\(params\) \{[\s\S]*?\n\}\n(?=function shouldClearUnboundScopesForMissingDeviceIdentity\(params\) \{)/;
	if (!helperSectionPattern.test(source)) {
		throw new Error(`mediated auth helper replacement anchor not found in ${target}`);
	}
	source = source.replace(helperSectionPattern, helperBlock);
}

const decisionAnchor = '\tif (params.isControlUi && params.trustedProxyAuthOk) return { kind: "allow" };\n\tif (params.isControlUi && params.controlUiAuthPolicy.allowBypass && params.role === "operator") return { kind: "allow" };';
const oldMediatedDecision = '\tif (isGtManagerMediatedControlUiAuth(params)) return { kind: "allow" };';
const newMediatedDecision = '\tif (isGtManagerMediatedControlUiAuth(params)) return { kind: "allow", gtManagerMediatedControlUi: true };';
const decisionReplacement = `\tif (params.isControlUi && params.trustedProxyAuthOk) return { kind: "allow" };\n${newMediatedDecision}\n\tif (params.isControlUi && params.controlUiAuthPolicy.allowBypass && params.role === "operator") return { kind: "allow" };`;

if (source.includes(oldMediatedDecision)) {
	source = source.replace(oldMediatedDecision, newMediatedDecision);
} else if (!source.includes(newMediatedDecision)) {
	if (!source.includes(decisionAnchor)) {
		throw new Error(`missing-device decision anchor not found in ${target}`);
	}
	source = source.replace(decisionAnchor, decisionReplacement);
}

const scopeClearAnchor = 'function shouldClearUnboundScopesForMissingDeviceIdentity(params) {\n\treturn params.decision.kind !== "allow" || !params.controlUiAuthPolicy.allowBypass && !params.preserveInsecureLocalControlUiScopes && (params.authMethod === "token" || params.authMethod === "password" || params.authMethod === "trusted-proxy" || params.trustedProxyAuthOk === true);\n}';
const scopeClearReplacement = 'function shouldClearUnboundScopesForMissingDeviceIdentity(params) {\n\tif (params.decision?.gtManagerMediatedControlUi === true) return false;\n\treturn params.decision.kind !== "allow" || !params.controlUiAuthPolicy.allowBypass && !params.preserveInsecureLocalControlUiScopes && (params.authMethod === "token" || params.authMethod === "password" || params.authMethod === "trusted-proxy" || params.trustedProxyAuthOk === true);\n}';

if (!source.includes('params.decision?.gtManagerMediatedControlUi === true')) {
	if (!source.includes(scopeClearAnchor)) {
		throw new Error(`scope clearing anchor not found in ${target}`);
	}
	source = source.replace(scopeClearAnchor, scopeClearReplacement);
}

const scopeNormalizationAnchor = '\t\t\t\t\tif (!device && shouldClearUnboundScopesForMissingDeviceIdentity({\n\t\t\t\t\t\tdecision,\n\t\t\t\t\t\tcontrolUiAuthPolicy,\n\t\t\t\t\t\tpreserveInsecureLocalControlUiScopes,\n\t\t\t\t\t\tauthMethod,\n\t\t\t\t\t\ttrustedProxyAuthOk\n\t\t\t\t\t})) clearUnboundScopes();';
const scopeNormalizationReplacement = '\t\t\t\t\tif (!device && decision.gtManagerMediatedControlUi === true) {\n\t\t\t\t\t\tscopes = normalizeGtManagerMediatedControlUiScopes(scopes);\n\t\t\t\t\t\tconnectParams.scopes = scopes;\n\t\t\t\t\t}\n' + scopeNormalizationAnchor;

if (!source.includes('normalizeGtManagerMediatedControlUiScopes(scopes);')) {
	if (!source.includes(scopeNormalizationAnchor)) {
		throw new Error(`mediated scope normalization anchor not found in ${target}`);
	}
	source = source.replace(scopeNormalizationAnchor, scopeNormalizationReplacement);
}

const completeMissingDeviceCallPattern = /\n([ \t]+)trustedProxyAuthOk,\n\1sharedAuthOk,\n\1authOk,\n\1authMethod,\n\1forwardedPrefix: upgradeReq\.headers\?\.\["x-forwarded-prefix"\],\n\1hasSharedAuth,/;
if (!completeMissingDeviceCallPattern.test(source)) {
	const missingAuthMethodCallPattern = /(\n([ \t]+)trustedProxyAuthOk,\n\2sharedAuthOk,\n\2authOk,\n)(\2forwardedPrefix: upgradeReq\.headers\?\.\["x-forwarded-prefix"\],\n\2hasSharedAuth,)/;
	const missingBothCallPattern = /(\n([ \t]+)trustedProxyAuthOk,\n\2sharedAuthOk,\n\2authOk,\n)(\2hasSharedAuth,)/;
	if (missingAuthMethodCallPattern.test(source)) {
		source = source.replace(missingAuthMethodCallPattern, (_match, prefix, indent, rest) => `${prefix}${indent}authMethod,\n${rest}`);
	} else if (missingBothCallPattern.test(source)) {
		source = source.replace(missingBothCallPattern, (_match, prefix, indent, rest) => `${prefix}${indent}authMethod,\n${indent}forwardedPrefix: upgradeReq.headers?.["x-forwarded-prefix"],\n${rest}`);
	} else {
		throw new Error(`evaluateMissingDeviceIdentity authMethod/forwardedPrefix call anchor not found in ${target}`);
	}
}

fs.writeFileSync(target, source);
