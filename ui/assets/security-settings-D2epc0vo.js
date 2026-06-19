import { s as makeRestApiRequest } from "./useRootStore-DPMB1D8p.js";
//#region ../@n8n/rest-api-client/src/api/security-settings.ts
async function getSecuritySettings(context) {
	return await makeRestApiRequest(context, "GET", "/settings/security");
}
async function updateSecuritySettings(context, data) {
	return await makeRestApiRequest(context, "POST", "/settings/security", data);
}
//#endregion
export { updateSecuritySettings as n, getSecuritySettings as t };
