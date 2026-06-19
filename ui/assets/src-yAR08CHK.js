import { i as init_dist } from "./empty-B-xA4Kx0.js";
import { h as NANOID_ALPHABET } from "./useRootStore-DPMB1D8p.js";
//#region ../../@n8n/utils/src/event-queue.ts
/**
* Create an event queue that processes events sequentially.
*
* @param processEvent - Async function that processes a single event.
* @returns A function that enqueues events for processing.
*/
function createEventQueue(processEvent) {
	const queue = [];
	let processing = false;
	/**
	* Process the next event in the queue (if not already processing).
	*/
	async function processNext() {
		if (processing || queue.length === 0) return;
		processing = true;
		const currentEvent = queue.shift();
		if (currentEvent !== void 0) try {
			await processEvent(currentEvent);
		} catch (error) {
			console.error("Error processing event:", error);
		}
		processing = false;
		await processNext();
	}
	/**
	* Enqueue an event and trigger processing.
	*
	* @param event - The event to enqueue.
	*/
	function enqueue(event) {
		queue.push(event);
		processNext();
	}
	return { enqueue };
}
//#endregion
//#region ../../../node_modules/.pnpm/nanoid@3.3.8/node_modules/nanoid/index.browser.js
var random = (bytes) => crypto.getRandomValues(new Uint8Array(bytes));
var customRandom = (alphabet, defaultSize, getRandom) => {
	let mask = (2 << Math.log(alphabet.length - 1) / Math.LN2) - 1;
	let step = -~(1.6 * mask * defaultSize / alphabet.length);
	return (size = defaultSize) => {
		let id = "";
		while (true) {
			let bytes = getRandom(step);
			let j = step | 0;
			while (j--) {
				id += alphabet[bytes[j] & mask] || "";
				if (id.length === size) return id;
			}
		}
	};
};
var customAlphabet = (alphabet, size = 21) => customRandom(alphabet, size, random);
var nanoid = (size = 21) => crypto.getRandomValues(new Uint8Array(size)).reduce((id, byte) => {
	byte &= 63;
	if (byte < 36) id += byte.toString(36);
	else if (byte < 62) id += (byte - 26).toString(36).toUpperCase();
	else if (byte > 62) id += "-";
	else id += "_";
	return id;
}, "");
//#endregion
//#region ../../@n8n/utils/src/workflowId.ts
/**
* Generates a unique 16-character nanoid.
*
* This is the canonical ID generator used across the entire n8n codebase for:
* - Workflow IDs
* - Project IDs
* - Variable IDs
* - API Key IDs
* - And other entity IDs
*
* Both frontend and backend MUST use this function to ensure consistency.
*
* @returns A 16-character ID
*
* @example
* ```ts
* const id = generateNanoId();
* // => 'aBcDeFgHiJkLmNoP' (16 characters)
* ```
*/
var generateNanoId = customAlphabet(NANOID_ALPHABET, 16);
//#endregion
//#region ../../@n8n/utils/src/sort/sortByProperty.ts
var sortByProperty = (property, arr, order = "asc") => arr.sort((a, b) => {
	const result = String(a[property]).localeCompare(String(b[property]), void 0, {
		numeric: true,
		sensitivity: "base"
	});
	return order === "asc" ? result : -result;
});
//#endregion
//#region ../../@n8n/utils/src/files/sanitize.ts
var INVALID_CHARS_REGEX = /[<>:"/\\|?*\u0000-\u001F\u007F-\u009F]/g;
var ZERO_WIDTH_CHARS_REGEX = /[\u200B-\u200D\u2060\uFEFF]/g;
var UNICODE_SPACES_REGEX = /[\u00A0\u2000-\u200A]/g;
var LEADING_TRAILING_DOTS_SPACES_REGEX = /^[\s.]+|[\s.]+$/g;
var WINDOWS_RESERVED_NAMES = new Set([
	"CON",
	"PRN",
	"AUX",
	"NUL",
	"COM1",
	"COM2",
	"COM3",
	"COM4",
	"COM5",
	"COM6",
	"COM7",
	"COM8",
	"COM9",
	"LPT1",
	"LPT2",
	"LPT3",
	"LPT4",
	"LPT5",
	"LPT6",
	"LPT7",
	"LPT8",
	"LPT9"
]);
var DEFAULT_FALLBACK_NAME = "untitled";
var MAX_FILENAME_LENGTH = 200;
/**
* Sanitizes a filename to be compatible with Mac, Linux, and Windows file systems
*
* Main features:
* - Replace invalid characters (e.g. ":" in hello:world)
* - Handle Windows reserved names
* - Limit filename length
* - Normalize Unicode characters
*
* @param filename - The filename to sanitize (without extension)
* @param maxLength - Maximum filename length (default: 200)
* @returns A sanitized filename (without extension)
*
* @example
* sanitizeFilename('hello:world') // returns 'hello_world'
* sanitizeFilename('CON') // returns '_CON'
* sanitizeFilename('') // returns 'untitled'
*/
var sanitizeFilename = (filename, maxLength = MAX_FILENAME_LENGTH) => {
	if (!filename) return DEFAULT_FALLBACK_NAME;
	let baseName = filename.trim().replace(INVALID_CHARS_REGEX, "_").replace(ZERO_WIDTH_CHARS_REGEX, "").replace(UNICODE_SPACES_REGEX, " ").replace(LEADING_TRAILING_DOTS_SPACES_REGEX, "");
	if (!baseName) baseName = DEFAULT_FALLBACK_NAME;
	if (WINDOWS_RESERVED_NAMES.has(baseName.toUpperCase())) baseName = `_${baseName}`;
	if (baseName.length > maxLength) baseName = baseName.slice(0, maxLength);
	return baseName;
};
//#endregion
//#region ../../@n8n/utils/src/placeholder.ts
var PLACEHOLDER_REGEX = /<__PLACEHOLDER.*?__>/;
/** Checks if a value is a placeholder value (matches the placeholder regex pattern). */
function isPlaceholderValue(value) {
	if (typeof value !== "string") return false;
	return !!value.match(PLACEHOLDER_REGEX);
}
/**
* Extracts the label from a single placeholder string.
* Handles formats like:
* - <__PLACEHOLDER_VALUE__label__>
* - <__PLACEHOLDER__: label__>
*/
function extractLabelFromPlaceholder(placeholder) {
	let label = placeholder.slice(14, -3);
	if (label.startsWith("_VALUE__")) label = label.slice(8);
	else if (label.startsWith("__:")) label = label.slice(3);
	else if (label.startsWith("__")) label = label.slice(2);
	return label.trim();
}
/**
* Extracts all placeholder labels from a string value.
* Handles both cases where the entire value is a placeholder and where
* placeholders are embedded within code (e.g., Code node).
* Returns an array of labels found.
*/
function extractPlaceholderLabels(value) {
	if (typeof value !== "string") return [];
	const labels = [];
	const regex = new RegExp(PLACEHOLDER_REGEX.source, "g");
	let match;
	while ((match = regex.exec(value)) !== null) {
		const label = extractLabelFromPlaceholder(match[0]);
		if (label.length > 0) labels.push(label);
	}
	return labels;
}
/**
* Recursively searches through a value (object, array, or primitive) to find
* all placeholder values and their paths.
*/
function findPlaceholderDetails(value, path = []) {
	if (typeof value === "string") return extractPlaceholderLabels(value).map((label) => ({
		path,
		label
	}));
	if (Array.isArray(value)) return value.flatMap((item, index) => findPlaceholderDetails(item, [...path, `[${index}]`]));
	if (value !== null && typeof value === "object") return Object.entries(value).flatMap(([key, nested]) => findPlaceholderDetails(nested, [...path, key]));
	return [];
}
/**
* Formats a path array into a dot-notation string for display.
* Array indices are preserved as [N] without leading dots.
*/
function formatPlaceholderPath(path) {
	if (path.length === 0) return "parameters";
	return path.map((segment, index) => segment.startsWith("[") || index === 0 ? segment : `.${segment}`).join("");
}
//#endregion
//#region ../../@n8n/utils/src/jwt.ts
init_dist();
//#endregion
//#region ../../@n8n/utils/src/scrub-secrets.ts
/**
* Replace common credential patterns in free-form text with `[REDACTED]`.
*
* Used before persisting or transmitting user-supplied text (telemetry
* excerpts, eval report HTML, free-form feedback) where keys/tokens
* accidentally pasted into prompts or command lines could otherwise leak
* downstream.
*
* Conservative by design: matches well-known prefixed tokens, explicit
* `key=value` pairs, and quoted JSON/JS-object fields with sensitive
* names. We don't attempt to redact arbitrary long opaque strings — false
* positives on file paths, IDs, or base64 payloads would make the output
* unreadable.
*/
var SECRET_KEYS = "password|passwd|secret|credentials?|api[_-]?key|authorization|access[_-]?token|refresh[_-]?token|id[_-]?token|session[_-]?token|auth[_-]?token";
var SECRET_VALUE_PATTERNS = [
	/\b(?:Bearer|Basic|Token)\s+[A-Za-z0-9._~+/=-]{12,}/gi,
	/\bsk-(?:ant-|proj-)?[A-Za-z0-9_-]{16,}/g,
	/\bxox[abprso]-[A-Za-z0-9-]{10,}/g,
	/\bgh[psoru]_[A-Za-z0-9]{20,}/g,
	/\bAKIA[0-9A-Z]{16}\b/g,
	new RegExp(`"(?:${SECRET_KEYS})"\\s*:\\s*"(?!\\[(?:redacted|REDACTED)\\]")(?:\\\\.|[^"\\r\\n])*"`, "gi"),
	new RegExp(`'(?:${SECRET_KEYS})'\\s*:\\s*'(?!\\[(?:redacted|REDACTED)\\]')(?:\\\\.|[^'\\r\\n])*'`, "gi"),
	new RegExp(`\\b(?:${SECRET_KEYS})\\s*[:=]\\s*\\S+`, "gi")
];
function scrubSecretsInText(input) {
	let out = input;
	for (const pattern of SECRET_VALUE_PATTERNS) out = out.replace(pattern, "[REDACTED]");
	return out;
}
//#endregion
export { isPlaceholderValue as a, generateNanoId as c, formatPlaceholderPath as i, nanoid as l, extractPlaceholderLabels as n, sanitizeFilename as o, findPlaceholderDetails as r, sortByProperty as s, scrubSecretsInText as t, createEventQueue as u };
