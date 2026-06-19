import { S as computed } from "./vue.runtime.esm-bundler-_seTmgvI.js";
import { in as useNDVStore, t as useWorkflowsStore, v as createWorkflowDocumentId, x as useWorkflowDocumentStore } from "./workflows.store-DcZL_sW6.js";
import { S as STORES, T as defineStore, t as useRootStore } from "./useRootStore-DPMB1D8p.js";
import { t as useSettingsStore } from "./settings.store-CzH-DVeq.js";
import { _ as useUIStore, t as useUsersStore } from "./users.store-Cqhh8Mpv.js";
//#region src/app/stores/webhooks.store.ts
var useWebhooksStore = defineStore(STORES.WEBHOOKS, () => {
	const workflowsStore = useWorkflowsStore();
	const workflowDocumentStore = computed(() => useWorkflowDocumentStore(createWorkflowDocumentId(workflowsStore.workflowId)));
	const ndvStore = computed(() => useNDVStore(workflowDocumentStore.value.documentId));
	return {
		...useRootStore(),
		...useWorkflowsStore(),
		...useUIStore(),
		...useUsersStore(),
		workflowDocumentStore,
		ndvStore,
		...useSettingsStore()
	};
});
//#endregion
//#region src/app/composables/useExternalHooks.ts
async function runExternalHook(eventName, metadata) {
	if (!window.n8nExternalHooks) return;
	const store = useWebhooksStore();
	const [resource, operator] = eventName.split(".");
	const context = window.n8nExternalHooks[resource];
	if (context?.[operator]) {
		const hookMethods = context[operator];
		for (const hookMethod of hookMethods) await hookMethod(store, metadata);
	}
}
function useExternalHooks() {
	return { run: runExternalHook };
}
//#endregion
export { useExternalHooks as t };
