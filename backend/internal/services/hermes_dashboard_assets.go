package services

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var hermesDashboardAssetBase = regexp.MustCompile(`^/api/v1/instances/[1-9][0-9]*/proxy$`)

// Rewrite only static resource URL tokens, never arbitrary text or JavaScript.
// Native relative ES imports already inherit the proxied entry module URL.
func hermesDashboardAssetReference(value, prefix string) string {
	if !hermesDashboardAssetBase.MatchString(prefix) || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return value
	}
	u, err := url.Parse(value)
	if err != nil || u.RawPath != "" || !hermesDashboardCanonicalPath(u.Path) || (u.Path != "/favicon.ico" && !hermesDashboardAsset.MatchString(u.Path)) {
		return value
	}
	return prefix + value
}

func rewriteHermesDashboardHTMLAssets(body, prefix string) string {
	if !hermesDashboardAssetBase.MatchString(prefix) {
		return body
	}
	var out bytes.Buffer
	tokens := html.NewTokenizer(strings.NewReader(body))
	headEnd, hasBridge, inStyle := -1, false, false
	for {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			if tokens.Err() != io.EOF {
				return body
			}
			break
		}
		raw := bytes.Clone(tokens.Raw())
		switch kind {
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			changed := false
			for i := range token.Attr {
				attribute := &token.Attr[i]
				if token.Data == "script" && attribute.Key == "id" && attribute.Val == "cm-hermes-assets" {
					hasBridge = true
				}
				value := attribute.Val
				if (token.Data == "link" && attribute.Key == "href") ||
					(attribute.Key == "src" && strings.Contains(" script img source video audio input track ", " "+token.Data+" ")) ||
					(token.Data == "video" && attribute.Key == "poster") {
					attribute.Val = hermesDashboardAssetReference(value, prefix)
				} else if attribute.Key == "style" {
					attribute.Val = rewriteHermesDashboardCSSAssets(value, prefix)
				}
				changed = changed || value != attribute.Val
			}
			if changed {
				out.WriteString(token.String())
			} else {
				out.Write(raw)
			}
			if token.Data == "head" && headEnd < 0 {
				headEnd = out.Len()
			}
			inStyle = token.Data == "style"
		case html.EndTagToken:
			if tokens.Token().Data == "style" {
				inStyle = false
			}
			out.Write(raw)
		case html.TextToken:
			if inStyle {
				out.WriteString(rewriteHermesDashboardCSSAssets(string(raw), prefix))
			} else {
				out.Write(raw)
			}
		default:
			out.Write(raw)
		}
	}
	result := out.String()
	if hasBridge {
		return result
	}
	if headEnd < 0 {
		headEnd = 0
	}
	prefixJSON, _ := json.Marshal(prefix)
	bridge := strings.Replace(hermesDashboardAssetBridge, "__CM_HERMES_ASSET_BASE__", string(prefixJSON), 1)
	return result[:headEnd] + bridge + result[headEnd:]
}

// Vite 8's actual react-vendor bundle constructs root-absolute preload URLs
// and assigns link.href before appendChild. Native modulepreload/CSS requests
// bypass fetch/XHR. Rewrite at the URL setter (not a late MutationObserver),
// narrowly for same-origin static assets. API, navigation, foreign origins and
// already-prefixed/other-instance URLs are untouched. No bundle edits needed.
const hermesDashboardAssetBridge = `<script id="cm-hermes-assets">(function(p){
"use strict";
function fix(value){
 if(typeof value!=="string"&&!(value instanceof URL))return value;
 try{
  var u=new URL(String(value),document.baseURI);
  if(u.origin!==window.location.origin||(u.pathname!=="/favicon.ico"&&!/^\/(assets|fonts|fonts-terminal|ds-assets)\/[A-Za-z0-9_./-]+\.(js|css|woff2?|ttf|otf|svg|png|jpe?g|webp|avif|gif|ico|wasm)$/.test(u.pathname)))return value;
  return p+u.pathname+u.search+u.hash;
 }catch(e){return value;}
}
function resourceAttribute(node,name){
 return (node instanceof HTMLLinkElement&&name==="href")||(node instanceof HTMLScriptElement&&name==="src");
}
[[HTMLLinkElement.prototype,"href"],[HTMLScriptElement.prototype,"src"]].forEach(function(pair){
 var descriptor=Object.getOwnPropertyDescriptor(pair[0],pair[1]);
 if(!descriptor||!descriptor.set)return;
 var setter=descriptor.set;
 descriptor.set=function(value){setter.call(this,fix(value));};
 Object.defineProperty(pair[0],pair[1],descriptor);
});
var setAttribute=Element.prototype.setAttribute;
Element.prototype.setAttribute=function(name,value){
 if(resourceAttribute(this,String(name).toLowerCase()))value=fix(value);
 return setAttribute.call(this,name,value);
};
})(__CM_HERMES_ASSET_BASE__);</script>`

// CSS URL tokens are rewritten lexically. Comments, content strings, external
// URLs and non-resource paths stay byte-for-byte unchanged. Escaped URL forms
// are left untouched rather than decoded/rewritten with ambiguous semantics.
func rewriteHermesDashboardCSSAssets(css, prefix string) string {
	if !hermesDashboardAssetBase.MatchString(prefix) {
		return css
	}
	var out strings.Builder
	for i := 0; i < len(css); {
		if strings.HasPrefix(css[i:], "/*") {
			end := strings.Index(css[i+2:], "*/")
			if end < 0 {
				out.WriteString(css[i:])
				break
			}
			end += i + 4
			out.WriteString(css[i:end])
			i = end
			continue
		}
		if css[i] == '\'' || css[i] == '"' {
			end := hermesCSSStringEnd(css, i)
			out.WriteString(css[i:end])
			i = end
			continue
		}
		if i+7 <= len(css) && strings.EqualFold(css[i:i+7], "@import") && (i+7 == len(css) || !hermesCSSIdent(css[i+7])) {
			start := i + 7
			for start < len(css) && hermesCSSSpace(css[start]) {
				start++
			}
			if start < len(css) && (css[start] == '\'' || css[start] == '"') {
				end := hermesCSSStringEnd(css, start)
				if end > start+1 && css[end-1] == css[start] {
					value := css[start+1 : end-1]
					out.WriteString(css[i : start+1])
					out.WriteString(hermesCSSAssetValue(value, prefix))
					out.WriteByte(css[end-1])
					i = end
					continue
				}
			}
		}
		if i+3 <= len(css) && strings.EqualFold(css[i:i+3], "url") && (i == 0 || !hermesCSSIdent(css[i-1])) {
			open := i + 3
			for open < len(css) && hermesCSSSpace(css[open]) {
				open++
			}
			if open < len(css) && css[open] == '(' {
				end := open + 1
				for end < len(css) && css[end] != ')' {
					if css[end] == '\\' && end+1 < len(css) {
						end += 2
					} else if css[end] == '\'' || css[end] == '"' {
						end = hermesCSSStringEnd(css, end)
					} else {
						end++
					}
				}
				if end < len(css) {
					valueStart, valueEnd := open+1, end
					for valueStart < valueEnd && hermesCSSSpace(css[valueStart]) {
						valueStart++
					}
					for valueEnd > valueStart && hermesCSSSpace(css[valueEnd-1]) {
						valueEnd--
					}
					if valueEnd-valueStart >= 2 && (css[valueStart] == '\'' || css[valueStart] == '"') && css[valueEnd-1] == css[valueStart] {
						valueStart++
						valueEnd--
					}
					out.WriteString(css[i:valueStart])
					out.WriteString(hermesCSSAssetValue(css[valueStart:valueEnd], prefix))
					out.WriteString(css[valueEnd : end+1])
					i = end + 1
					continue
				}
			}
		}
		out.WriteByte(css[i])
		i++
	}
	return out.String()
}

func hermesCSSStringEnd(css string, start int) int {
	for i := start + 1; i < len(css); i++ {
		if css[i] == '\\' {
			i++
		} else if css[i] == css[start] {
			return i + 1
		}
	}
	return len(css)
}
func hermesCSSIdent(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}
func hermesCSSSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' }
func hermesCSSAssetValue(value, prefix string) string {
	if strings.Contains(value, "\\") {
		return value
	}
	return hermesDashboardAssetReference(value, prefix)
}
