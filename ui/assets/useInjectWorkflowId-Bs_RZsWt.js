import { Zl as WorkflowIdKey } from "./constants-CqN-rIyX.js";
import { xr as injectStrict } from "./workflows.store-DcZL_sW6.js";
//#region src/app/composables/useInjectWorkflowId.ts
function useInjectWorkflowId() {
	return injectStrict(WorkflowIdKey);
}
//#endregion
export { useInjectWorkflowId as t };
