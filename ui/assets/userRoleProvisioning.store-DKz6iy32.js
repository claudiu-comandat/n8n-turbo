import { Ft as readonly, It as ref } from "./vue.runtime.esm-bundler-_seTmgvI.js";
import { T as defineStore, s as makeRestApiRequest, t as useRootStore } from "./useRootStore-DPMB1D8p.js";
//#region ../@n8n/rest-api-client/src/api/provisioning.ts
var getProvisioningConfig = async (context) => {
	return await makeRestApiRequest(context, "GET", "/sso/provisioning/config");
};
var saveProvisioningConfig = async (context, config) => {
	return await makeRestApiRequest(context, "PATCH", "/sso/provisioning/config", config);
};
//#endregion
//#region src/features/settings/sso/provisioning/composables/userRoleProvisioning.store.ts
/**
* Composable to load and save provisioning config
*/
var useUserRoleProvisioningStore = defineStore("userRoleProvisioning", () => {
	const rootStore = useRootStore();
	const provisioningConfig = ref();
	const getProvisioningConfig$1 = async () => {
		try {
			const config = await getProvisioningConfig(rootStore.restApiContext);
			provisioningConfig.value = config;
			return config;
		} catch (error) {
			console.error("Failed to fetch provisioning config:", error);
			throw error;
		}
	};
	const saveProvisioningConfig$1 = async (config) => {
		try {
			const updatedConfig = await saveProvisioningConfig(rootStore.restApiContext, config);
			provisioningConfig.value = updatedConfig;
			return updatedConfig;
		} catch (error) {
			console.error("Failed to save provisioning config:", error);
			throw error;
		}
	};
	return {
		provisioningConfig: readonly(provisioningConfig),
		getProvisioningConfig: getProvisioningConfig$1,
		saveProvisioningConfig: saveProvisioningConfig$1
	};
});
//#endregion
export { useUserRoleProvisioningStore as t };
