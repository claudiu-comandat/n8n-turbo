import { $ as openBlock, N as defineComponent, X as onMounted, at as resolveComponent, bt as withCtx, j as createVNode, q as onBeforeUnmount, w as createBlock } from "./vue.runtime.esm-bundler-_seTmgvI.js";
import { t as BaseLayout_default } from "./BaseLayout-HpZqPjrt.js";
import { t as usePushConnectionStore } from "./pushConnection.store-BteXwrQS.js";
import { t as AppSidebar_default } from "./AppSidebar-CVr7_FyN.js";
//#endregion
//#region src/app/layouts/InstanceAiLayout.vue
var InstanceAiLayout_default = /* @__PURE__ */ defineComponent({
	__name: "InstanceAiLayout",
	setup(__props) {
		const pushConnectionStore = usePushConnectionStore();
		onMounted(() => {
			pushConnectionStore.pushConnect();
		});
		onBeforeUnmount(() => {
			pushConnectionStore.pushDisconnect();
		});
		return (_ctx, _cache) => {
			const _component_RouterView = resolveComponent("RouterView");
			return openBlock(), createBlock(BaseLayout_default, null, {
				sidebar: withCtx(() => [createVNode(AppSidebar_default)]),
				default: withCtx(() => [createVNode(_component_RouterView)]),
				_: 1
			});
		};
	}
});
//#endregion
export { InstanceAiLayout_default as default };
