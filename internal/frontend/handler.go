package frontend

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/security"
)

type Config struct {
	UIPath      string
	EmbedFS     fs.FS
	CacheMaxAge time.Duration
	DevMode     bool
	CSP         CSPConfig
	BasePath    string
}

type Handler struct {
	config     Config
	fileServer http.Handler
	files      fs.FS
}

func NewHandler(cfg Config) (*Handler, error) {
	if cfg.UIPath == "" {
		cfg.UIPath = os.Getenv("UI_PATH")
	}
	if cfg.UIPath == "" {
		cfg.UIPath = "./ui"
	}
	if cfg.CacheMaxAge == 0 {
		cfg.CacheMaxAge = 365 * 24 * time.Hour
	}
	if !cfg.CSP.Enabled && !cfg.DevMode {
		cfg.CSP = DefaultCSP("")
	}
	cfg.BasePath = normalizeBasePath(cfg.BasePath)
	files := cfg.EmbedFS
	if files == nil {
		files = os.DirFS(cfg.UIPath)
	}
	return &Handler{
		config:     cfg,
		fileServer: http.FileServer(http.FS(files)),
		files:      files,
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := normalizeAssetPath(r.URL.Path)
	if strings.HasPrefix(path, "/rest/") || strings.HasPrefix(path, "/webhook/") || strings.HasPrefix(path, "/webhook-test/") || path == "/metrics" || path == "/healthz" {
		http.NotFound(w, r)
		return
	}
	h.setSecurityHeaders(w)
	if filepath.Ext(path) != "" {
		if !h.fileExists(path) {
			h.serveIndex(w, r)
			return
		}
		h.setCacheHeaders(w, path)
		h.serveAsset(w, r, path)
		return
	}
	h.serveIndex(w, r)
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Clear-Site-Data", `"cache"`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	ApplyCSP(h.config.CSP, w)
	content, err := fs.ReadFile(h.files, "index.html")
	if err != nil {
		http.Error(w, fmt.Sprintf("index.html unavailable: %v", err), http.StatusNotFound)
		return
	}
	_, _ = w.Write([]byte(h.replacePlaceholders(string(content))))
}

func (h *Handler) serveAsset(w http.ResponseWriter, r *http.Request, path string) {
	if h.servesTransformedAsset(w, path) {
		return
	}
	servePath := path
	if acceptsGzip(r) && h.fileExists(path+".gz") {
		servePath = path + ".gz"
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		if contentType := mime.TypeByExtension(filepath.Ext(path)); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = servePath
	h.fileServer.ServeHTTP(w, r2)
}

func (h *Handler) servesTransformedAsset(w http.ResponseWriter, path string) bool {
	clean := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "/")
	if clean == "static/n8n-turbo-compat.js" {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write([]byte(turboCompatibilityScript()))
		return true
	}
	if clean == "static/base-path.js" {
		content, err := fs.ReadFile(h.files, clean)
		if err != nil {
			return false
		}
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write([]byte(h.replacePlaceholders(string(content))))
		return true
	}

	ext := strings.ToLower(filepath.Ext(clean))
	if ext != ".js" && ext != ".css" {
		return false
	}

	content, err := fs.ReadFile(h.files, clean)
	if err != nil {
		return false
	}
	raw := string(content)
	transformed := h.replacePlaceholders(raw)
	if transformed == raw {
		return false
	}
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	if contentType := mime.TypeByExtension(ext); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	_, _ = w.Write([]byte(transformed))
	return true
}

func (h *Handler) setCacheHeaders(w http.ResponseWriter, path string) {
	if h.config.DevMode {
		w.Header().Set("Cache-Control", "no-cache")
		return
	}
	if isVersionedAsset(path) {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d, immutable", int(h.config.CacheMaxAge.Seconds())))
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
}

func (h *Handler) fileExists(path string) bool {
	clean := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "/")
	if clean == "." || strings.HasPrefix(clean, "../") {
		return false
	}
	if clean == "static/n8n-turbo-compat.js" {
		return true
	}
	info, err := fs.Stat(h.files, clean)
	return err == nil && !info.IsDir()
}

func (h *Handler) replacePlaceholders(content string) string {
	basePath := strings.Trim(h.config.BasePath, "/")
	assetPrefix := "/"
	if basePath != "" {
		assetPrefix = "/" + basePath + "/"
	}
	content = strings.ReplaceAll(content, "%CONFIG_TAGS%", "")
	content = strings.ReplaceAll(content, "/{{BASE_PATH}}/", assetPrefix)
	content = strings.ReplaceAll(content, "'/{{BASE_PATH}}/'", "'"+assetPrefix+"'")
	content = strings.ReplaceAll(content, "{{BASE_PATH}}", basePath)
	content = strings.ReplaceAll(content, "/%7B%7BBASE_PATH%7D%7D/", assetPrefix)
	content = strings.ReplaceAll(content, "%7B%7BBASE_PATH%7D%7D", basePath)
	content = h.applyTurboFrontendCompatibility(content)
	content = h.injectTurboCompatibilityScript(content)
	return content
}

func (h *Handler) applyTurboFrontendCompatibility(content string) string {
	const repositionImportedWorkflow = `workflowHelpers.updateNodePositions(workflowData, getNewNodePosition(workflowDocumentStore.value.allNodes, lastClickPosition.value, {
				...workflowData.nodes && workflowData.nodes.length > 1 ? { size: getNodesGroupSize(workflowData.nodes) } : {},
				viewport
			}));`
	const repositionOnlyDuplicates = `if (source === "duplicate") workflowHelpers.updateNodePositions(workflowData, getNewNodePosition(workflowDocumentStore.value.allNodes, lastClickPosition.value, {
				...workflowData.nodes && workflowData.nodes.length > 1 ? { size: getNodesGroupSize(workflowData.nodes) } : {},
				viewport
			}));`
	content = strings.Replace(content, repositionImportedWorkflow, repositionOnlyDuplicates, 1)

	const webhookURLWithNullableWorkflowID = "function getNodeWebhookUrl(baseUrl, workflowId, node, path, isFullPath) {\n" +
		"\tif ((path.startsWith(\":\") || path.includes(\"/:\")) && node.webhookId) isFullPath = false;\n" +
		"\tif (path.startsWith(\"/\")) path = path.slice(1);\n" +
		"\treturn `${baseUrl}/${getNodeWebhookPath(workflowId, node, path, isFullPath)}`;\n" +
		"}"
	const webhookURLWithRouteFallback = "function getNodeWebhookUrl(baseUrl, workflowId, node, path, isFullPath) {\n" +
		"\tif (workflowId == null || workflowId === \"\") {\n" +
		"\t\tconst match = globalThis.location?.pathname?.match(/\\/workflow\\/([^/]+)/);\n" +
		"\t\tif (match?.[1]) workflowId = decodeURIComponent(match[1]);\n" +
		"\t}\n" +
		"\tif ((path.startsWith(\":\") || path.includes(\"/:\")) && node.webhookId) isFullPath = false;\n" +
		"\tif (path.startsWith(\"/\")) path = path.slice(1);\n" +
		"\treturn `${baseUrl}/${getNodeWebhookPath(workflowId, node, path, isFullPath)}`;\n" +
		"}"
	content = strings.Replace(content, webhookURLWithNullableWorkflowID, webhookURLWithRouteFallback, 1)

	const workflowStoreWebhookID = "const workflowId = workflowsStore.workflowId;\n" +
		"\t\tconst path = await getWebhookExpressionValue(webhookData, \"path\", true, node.name) ?? \"\";"
	const workflowStoreWebhookIDFallback = "let workflowId = workflowsStore.workflowId;\n" +
		"\t\tif (workflowId == null || workflowId === \"\" || workflowId === \"null\" || workflowId === \"undefined\") {\n" +
		"\t\t\tconst match = globalThis.location?.pathname?.match(/\\/workflow\\/([^/]+)/);\n" +
		"\t\t\tif (match?.[1]) workflowId = decodeURIComponent(match[1]);\n" +
		"\t\t}\n" +
		"\t\tconst path = await getWebhookExpressionValue(webhookData, \"path\", true, node.name) ?? \"\";"
	content = strings.Replace(content, workflowStoreWebhookID, workflowStoreWebhookIDFallback, 1)

	const workflowStoreInlineWebhookID = `const workflowId = workflowsStore.workflowId;
		return getNodeWebhookUrl(baseUrl, workflowId, node, await getWebhookExpressionValue(webhookData, "path", true, node.name) ?? "", await getWebhookExpressionValue(webhookData, "isFullPath", true, node.name) || false);`
	const workflowStoreInlineWebhookIDFallback = `let workflowId = workflowsStore.workflowId;
		if (workflowId == null || workflowId === "" || workflowId === "null" || workflowId === "undefined") {
			const match = globalThis.location?.pathname?.match(/\/workflow\/([^/]+)/);
			if (match?.[1]) workflowId = decodeURIComponent(match[1]);
		}
		return getNodeWebhookUrl(baseUrl, workflowId, node, await getWebhookExpressionValue(webhookData, "path", true, node.name) ?? "", await getWebhookExpressionValue(webhookData, "isFullPath", true, node.name) || false);`
	content = strings.Replace(content, workflowStoreInlineWebhookID, workflowStoreInlineWebhookIDFallback, 1)

	const triggerPanelWebhookComputed = `const webhookTestUrl = computedAsync(async () => {
			if (!node.value || !nodeType.value?.webhooks?.length) return;
			return await workflowHelpers.getWebhookUrl(nodeType.value.webhooks[0], node.value, "test");
		}, void 0);`
	const triggerPanelWebhookComputedFallback = `const webhookTestUrl = computedAsync(async () => {
			if (!node.value || !nodeType.value?.webhooks?.length) return;
			const url = await workflowHelpers.getWebhookUrl(nodeType.value.webhooks[0], node.value, "test");
			if (typeof url === "string" && url.includes("/null")) {
				const fallbackPath = node.value.webhookId || node.value.parameters?.path || "";
				return url.replace(/\/null(?=$|[?#])/, "/" + fallbackPath);
			}
			return url;
		}, void 0);`
	content = strings.Replace(content, triggerPanelWebhookComputed, triggerPanelWebhookComputedFallback, 1)
	return content
}

func (h *Handler) injectTurboCompatibilityScript(content string) string {
	const marker = "</head>"
	if !strings.Contains(content, marker) || strings.Contains(content, "N8N_TURBO_LOGS_PANEL_RESET_V1") {
		return content
	}
	script := `<script src="/static/n8n-turbo-compat.js"></script>`
	return strings.Replace(content, marker, script+marker, 1)
}

func turboCompatibilityScript() string {
	return `
try {
  if (localStorage.getItem('N8N_TURBO_LOGS_PANEL_RESET_V1') !== '1') {
    localStorage.removeItem('N8N_LOGS_PANEL_OPEN');
    localStorage.removeItem('N8N_CANVAS_CHAT_HEIGHT');
    localStorage.setItem('N8N_TURBO_LOGS_PANEL_RESET_V1', '1');
  }
} catch (_) {}

const __turboNativeSetTimeout = globalThis.setTimeout.bind(globalThis);
const __turboNativeSetInterval = globalThis.setInterval.bind(globalThis);

function __turboCallsite() {
  try {
    throw new Error();
  } catch (error) {
    return String(error?.stack || '');
  }
}

function __turboSlowTimerDelay(delay, stack) {
  if (typeof delay !== 'number' || !Number.isFinite(delay)) return delay;
  if (stack.includes('useBackendStatus')) {
    return Math.max(delay * 3, 30000);
  }
  if (
    stack.includes('executions.store') ||
    stack.includes('WorkflowExecutionsView') ||
    stack.includes('ExecutionsView')
  ) {
    return Math.max(delay * 4, 15000);
  }
  return delay;
}

globalThis.setTimeout = function (handler, timeout, ...args) {
  return __turboNativeSetTimeout(handler, __turboSlowTimerDelay(timeout, __turboCallsite()), ...args);
};

globalThis.setInterval = function (handler, timeout, ...args) {
  return __turboNativeSetInterval(handler, __turboSlowTimerDelay(timeout, __turboCallsite()), ...args);
};

function patchWebhookUrlPreview() {
  try {
    const url = document.querySelector('.node-webhooks .webhook-url');
    if (!url) return;
    const pathInput = document.querySelector('[data-parameter-path="parameters.path"] input, input[title*="Parameter: \\"path\\""]');
    const path = pathInput && typeof pathInput.value === 'string' ? pathInput.value.trim() : '';
    if (!path) return;
    const productionRadio = document.querySelector('[data-test-id="radio-button-production"]');
    const productionChecked = productionRadio?.closest('label')?.getAttribute('aria-checked') === 'true';
    const base = productionChecked ? location.origin + '/webhook/' : location.origin + '/webhook-test/';
    const next = base + path;
    if (url.textContent?.trim() !== next) {
      url.textContent = next;
      url.title = next;
    }
  } catch (_) {}
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', patchWebhookUrlPreview, { once: true });
} else {
  patchWebhookUrlPreview();
}

new MutationObserver(patchWebhookUrlPreview).observe(document.documentElement, {
  subtree: true,
  childList: true,
  characterData: true,
});
`
}

func normalizeBasePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "/")
	if path == "." {
		return ""
	}
	return path
}

func normalizeAssetPath(path string) string {
	path = strings.ReplaceAll(path, "/{{BASE_PATH}}/", "/")
	path = strings.ReplaceAll(path, "{{BASE_PATH}}/", "")
	path = strings.ReplaceAll(path, "{{BASE_PATH}}", "")
	path = strings.ReplaceAll(path, "/%7B%7BBASE_PATH%7D%7D/", "/")
	path = strings.ReplaceAll(path, "%7B%7BBASE_PATH%7D%7D/", "")
	path = strings.ReplaceAll(path, "%7B%7BBASE_PATH%7D%7D", "")
	if path == "" {
		return "/"
	}
	return path
}

func isVersionedAsset(path string) bool {
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	parts := strings.Split(base, "-")
	if len(parts) < 2 {
		return false
	}
	hash := parts[len(parts)-1]
	return len(hash) >= 8
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.TrimSpace(strings.Split(part, ";")[0]) == "gzip" {
			return true
		}
	}
	return false
}

func (h *Handler) setSecurityHeaders(w http.ResponseWriter) {
	security.ApplyHeaders(w.Header(), "", false)
}
