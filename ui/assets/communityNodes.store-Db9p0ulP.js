import { It as ref, S as computed } from "./vue.runtime.esm-bundler-_seTmgvI.js";
import { S as STORES, T as defineStore, a as get, l as post, s as makeRestApiRequest, t as useRootStore } from "./useRootStore-DPMB1D8p.js";
import { o as isAuthenticated } from "./users.store-Cqhh8Mpv.js";
//#region ../@n8n/rest-api-client/src/api/communityNodes.ts
async function getInstalledCommunityNodes(context) {
	return (await get(context.baseUrl, "/community-packages")).data || [];
}
async function installNewPackage(context, name, verify, version) {
	return await post(context.baseUrl, "/community-packages", {
		name,
		verify,
		version
	});
}
async function uninstallPackage(context, name) {
	return await makeRestApiRequest(context, "DELETE", "/community-packages", { name });
}
async function updatePackage(context, name, version, checksum) {
	return await makeRestApiRequest(context, "PATCH", "/community-packages", {
		name,
		version,
		checksum
	});
}
async function getAvailableCommunityPackageCount() {
	return (await get("https://api.npms.io/v2/", "search?q=keywords:n8n-community-node-package")).total || 0;
}
//#endregion
//#region src/features/settings/communityNodes/communityNodes.store.ts
var LOADER_DELAY = 300;
var useCommunityNodesStore = defineStore(STORES.COMMUNITY_NODES, () => {
	const availablePackageCount = ref(-1);
	const installedPackages = ref({});
	const rootStore = useRootStore();
	const getInstalledPackages = computed(() => {
		return Object.values(installedPackages.value).sort((a, b) => a.packageName.localeCompare(b.packageName));
	});
	const fetchAvailableCommunityPackageCount = async () => {
		if (availablePackageCount.value === -1) availablePackageCount.value = await getAvailableCommunityPackageCount();
	};
	const setInstalledPackages = (packages) => {
		installedPackages.value = packages.reduce((packageMap, pack) => {
			packageMap[pack.packageName] = pack;
			return packageMap;
		}, {});
	};
	const fetchInstalledPackages = async () => {
		if (!isAuthenticated()) return;
		const installedPackages = await getInstalledCommunityNodes(rootStore.restApiContext);
		setInstalledPackages(installedPackages);
		const timeout = installedPackages.length > 0 ? 0 : LOADER_DELAY;
		setTimeout(() => {}, timeout);
	};
	const installPackage = async (packageName, verify, version) => {
		await installNewPackage(rootStore.restApiContext, packageName, verify, version);
		await fetchInstalledPackages();
	};
	const uninstallPackage$1 = async (packageName) => {
		await uninstallPackage(rootStore.restApiContext, packageName);
		removePackageByName(packageName);
	};
	const removePackageByName = (name) => {
		const { [name]: removedPackage, ...remainingPackages } = installedPackages.value;
		installedPackages.value = remainingPackages;
	};
	const updatePackageObject = (newPackage) => {
		installedPackages.value[newPackage.packageName] = newPackage;
	};
	const updatePackage$1 = async (packageName, version, checksum) => {
		const packageToUpdate = installedPackages.value[packageName];
		updatePackageObject(await updatePackage(rootStore.restApiContext, packageToUpdate.packageName, version, checksum));
	};
	const getInstalledPackage = async (packageName) => {
		if (!getInstalledPackages.value.length) await fetchInstalledPackages();
		return getInstalledPackages.value.find((p) => p.packageName === packageName);
	};
	return {
		installedPackages,
		getInstalledPackage,
		getInstalledPackages,
		availablePackageCount,
		fetchAvailableCommunityPackageCount,
		fetchInstalledPackages,
		installPackage,
		uninstallPackage: uninstallPackage$1,
		updatePackage: updatePackage$1,
		setInstalledPackages
	};
});
//#endregion
export { useCommunityNodesStore as t };
