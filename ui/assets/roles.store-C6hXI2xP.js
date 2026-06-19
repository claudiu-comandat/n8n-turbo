import { It as ref, S as computed } from "./vue.runtime.esm-bundler-_seTmgvI.js";
import { Ol as PROJECT_OWNER_ROLE_SLUG } from "./constants-CqN-rIyX.js";
import { T as defineStore, s as makeRestApiRequest, t as useRootStore } from "./useRootStore-DPMB1D8p.js";
import { t as useSettingsStore } from "./settings.store-CzH-DVeq.js";
//#region ../@n8n/rest-api-client/src/api/roles.ts
var getRoles = async (context) => {
	return await makeRestApiRequest(context, "GET", "/roles?withUsageCount=true");
};
var createProjectRole = async (context, body) => {
	return await makeRestApiRequest(context, "POST", "/roles", body);
};
var getRoleBySlug = async (context, body) => {
	return await makeRestApiRequest(context, "GET", `/roles/${body.slug}?withUsageCount=true`);
};
var updateProjectRole = async (context, slug, body) => {
	return await makeRestApiRequest(context, "PATCH", `/roles/${slug}`, body);
};
var deleteProjectRole = async (context, slug) => {
	return await makeRestApiRequest(context, "DELETE", `/roles/${slug}`);
};
var getRoleAssignments = async (context, slug) => {
	return await makeRestApiRequest(context, "GET", `/roles/${slug}/assignments`);
};
var getRoleProjectMembers = async (context, slug, projectId) => {
	return await makeRestApiRequest(context, "GET", `/roles/${slug}/assignments/${projectId}/members`);
};
//#endregion
//#region src/app/stores/roles.store.ts
var useRolesStore = defineStore("roles", () => {
	const rootStore = useRootStore();
	const settingsStore = useSettingsStore();
	const roles = ref({
		global: [],
		project: [],
		credential: [],
		workflow: [],
		secretsProviderConnection: []
	});
	const projectRoleOrder = ref([
		"project:viewer",
		"project:chatUser",
		"project:editor",
		"project:admin"
	]);
	const projectRoleOrderMap = computed(() => new Map(projectRoleOrder.value.map((role, idx) => [role, idx])));
	const processedProjectRoles = computed(() => roles.value.project.filter((role) => role.slug !== PROJECT_OWNER_ROLE_SLUG).filter((role) => settingsStore.isChatFeatureEnabled || role.slug !== "project:chatUser").sort((a, b) => (projectRoleOrderMap.value.get(a.slug) ?? Number.MAX_SAFE_INTEGER) - (projectRoleOrderMap.value.get(b.slug) ?? Number.MAX_SAFE_INTEGER)));
	const processedCredentialRoles = computed(() => roles.value.credential.filter((role) => role.slug !== "credential:owner"));
	const processedWorkflowRoles = computed(() => roles.value.workflow.filter((role) => role.slug !== "workflow:owner"));
	const fetchRoles = async () => {
		roles.value = await getRoles(rootStore.restApiContext);
	};
	const createProjectRole$1 = async (body) => {
		return await createProjectRole(rootStore.restApiContext, body);
	};
	const fetchRoleBySlug = async (payload) => {
		return await getRoleBySlug(rootStore.restApiContext, payload);
	};
	const deleteProjectRole$1 = async (slug) => {
		return await deleteProjectRole(rootStore.restApiContext, slug);
	};
	const updateProjectRole$1 = async (slug, body) => {
		return await updateProjectRole(rootStore.restApiContext, slug, body);
	};
	const fetchRoleAssignments = async (slug) => {
		return await getRoleAssignments(rootStore.restApiContext, slug);
	};
	const fetchRoleProjectMembers = async (slug, projectId) => {
		return await getRoleProjectMembers(rootStore.restApiContext, slug, projectId);
	};
	return {
		roles,
		processedProjectRoles,
		processedCredentialRoles,
		processedWorkflowRoles,
		fetchRoles,
		createProjectRole: createProjectRole$1,
		fetchRoleBySlug,
		updateProjectRole: updateProjectRole$1,
		deleteProjectRole: deleteProjectRole$1,
		fetchRoleAssignments,
		fetchRoleProjectMembers
	};
});
//#endregion
export { useRolesStore as t };
