const __vite__mapDeps=(i,m=__vite__mapDeps,d=(m.f||(m.f=["assets/NodeCreator-DUXNyRCD.js","assets/_plugin-vue_export-helper-Da88TEg1.js","assets/chunk-CC9Q-vWm.js","assets/src-DydNw9MY.js","assets/get-CRtvjnJa.js","assets/_MapCache-Ca2fo2-Y.js","assets/vue.runtime.esm-bundler-_seTmgvI.js","assets/CalendarDate-DaCb8yxn.js","assets/sanitize-html-CoPbote2.js","assets/empty-B-xA4Kx0.js","assets/en-hN6GCUz2.js","assets/src-1s10hkmH.css","assets/src-BJJttHH5.js","assets/exports-CKHyusfv.js","assets/workflows.store-DcZL_sW6.js","assets/core-Dnh-TiO5.js","assets/constants-CqN-rIyX.js","assets/merge-Cmo4XKI-.js","assets/expression-runtime-stub-9w2ZHVM3.js","assets/useRootStore-DPMB1D8p.js","assets/settings.store-CzH-DVeq.js","assets/_baseOrderBy-yjAAZDv-.js","assets/useDebounce-CosXJuB-.js","assets/useDocumentTitle-DZdpZ-p7.js","assets/workflowsList.store-CRCJaw4U.js","assets/dateformat-9ZHpj92n.js","assets/users.store-Cqhh8Mpv.js","assets/canvas.eventBus-CG9T0dIH.js","assets/_virtual_node-popularity-data-D8fdYons.js","assets/useExternalHooks-BCllQN1q.js","assets/nodeIcon-CjwkSS4d.js","assets/templates.store-Cjw2o0HV.js","assets/cloudPlan.store-Cnql5JVh.js","assets/utils-Cd1Q9UIP.js","assets/NodeIcon-D1T23OfK.js","assets/NodeIcon-D6NjxeP8.css","assets/useAiGateway-Cn-M_vac.js","assets/builder.store-0L3N1NxY.js","assets/useMessage-DXevqXy7.js","assets/overlay-BT1FIYXU.js","assets/useToast-NYkrTq8y.js","assets/useStyles-CBXZLAO5.js","assets/FileSaver.min-CcOjHvdS.js","assets/src-yAR08CHK.js","assets/useCodeDiff-CcxmzqlN.js","assets/event-bus-DRyM_W_P.js","assets/useNodeHelpers-CyYcevDD.js","assets/useLoadingService-B93JbgYn.js","assets/useDynamicCredentials-DKIuNT0d.js","assets/focusPanel.store-VVqcjWHj.js","assets/useCalloutHelpers-BAPh37g_.js","assets/usePinnedData-CldfRuuu.js","assets/chatPanel.store-KPeBthKz.js","assets/assistant.store-CZTPILCN.js","assets/CommunityNodeUpdateInfo-D4hNOm6M.js","assets/semver-Bf7acRbf.js","assets/communityNodes.store-Db9p0ulP.js","assets/CommunityNodeUpdateInfo-BKXtPPI4.css","assets/useQuickConnect-CD5QucUP.js","assets/useCredentialOAuth-DZr47hit.js","assets/ContactAdministratorToInstall-CTSH7e-m.js","assets/useCanvasOperations-y0yiztbs.js","assets/core-B5QF79VM.js","assets/core-CH3PbqW3.js","assets/xml-DPJ9OCw0.js","assets/VueMarkdown-BIgl0KTf.js","assets/executions.store-DJWJirrV.js","assets/setupPanel.store-CNVcfpZJ.js","assets/templateTransforms-D4oxroIw.js","assets/useCanvasOperations-BGIelHPw.css","assets/ContactAdministratorToInstall-JoRxYxA6.css","assets/banners.store-vdTWbAcH.js","assets/banners-CXsyom4e.css","assets/useActions-fvAllbWc.js","assets/shield-alt-CQN2dq9I.js","assets/NodeCreator-sYDcv9PQ.css"])))=>i.map(i=>d[i]);
import { $ as openBlock, A as createTextVNode, C as createBaseVNode, Cn as toDisplayString, E as createElementBlock, Gt as unref, M as defineAsyncComponent, N as defineComponent, S as computed, T as createCommentVNode, W as nextTick, _ as Fragment, bt as withCtx, it as renderSlot, j as createVNode, v as Suspense, vn as normalizeClass, w as createBlock } from "./vue.runtime.esm-bundler-_seTmgvI.js";
import { s as useI18n } from "./src-BJJttHH5.js";
import { B as N8nPopover_default, Ja as N8nButton_default, Oi as AskAssistantIcon_default, Wi as N8nIconButton_default, zi as N8nTooltip_default } from "./src-DydNw9MY.js";
import { d as __vitePreload } from "./get-CRtvjnJa.js";
import { t as _plugin_vue_export_helper_default } from "./_plugin-vue_export-helper-Da88TEg1.js";
import { _t as NODE_CREATOR_OPEN_SOURCES, lr as STICKY_NODE_TYPE } from "./constants-CqN-rIyX.js";
import { Mr as getMidCanvasPosition } from "./workflows.store-DcZL_sW6.js";
import { _ as useUIStore, v as useTelemetry } from "./users.store-Cqhh8Mpv.js";
import { o as useWorkflowId } from "./builder.store-0L3N1NxY.js";
import { t as useFocusPanelStore } from "./focusPanel.store-VVqcjWHj.js";
import { t as useAssistantStore } from "./assistant.store-CZTPILCN.js";
import { t as useChatPanelStore } from "./chatPanel.store-KPeBthKz.js";
import { t as useSetupPanelStore } from "./setupPanel.store-CNVcfpZJ.js";
import { t as useEditorContext } from "./useEditorContext-CixTT5ma.js";
import { t as KeyboardShortcutTooltip_default } from "./KeyboardShortcutTooltip-BWemf5rc.js";
import { n as useNodeCreatorShortcutCoachmark } from "./useNodeCreatorShortcutCoachmark-Cun78zbT.js";
import { t as useActions } from "./useActions-fvAllbWc.js";
//#region src/features/shared/nodeCreator/components/NodeCreatorShortcutCoachmark.vue?vue&type=script&setup=true&lang.ts
var _hoisted_1 = { class: "node-creator-shortcut-coachmark__header" };
var _hoisted_2 = { class: "node-creator-shortcut-coachmark__title" };
var _hoisted_3 = { class: "node-creator-shortcut-coachmark__body" };
var _hoisted_4 = { class: "node-creator-shortcut-coachmark__footer" };
//#endregion
//#region src/features/shared/nodeCreator/components/NodeCreatorShortcutCoachmark.vue
var NodeCreatorShortcutCoachmark_default = /* @__PURE__ */ defineComponent({
	__name: "NodeCreatorShortcutCoachmark",
	props: { visible: { type: Boolean } },
	emits: ["dismiss"],
	setup(__props, { emit: __emit }) {
		const emit = __emit;
		const i18n = useI18n();
		return (_ctx, _cache) => {
			return openBlock(), createBlock(unref(N8nPopover_default), {
				open: __props.visible,
				"show-arrow": true,
				"enable-scrolling": false,
				"suppress-auto-focus": true,
				side: "left",
				align: "center",
				"side-offset": 8,
				"z-index": 2200,
				"content-class": "node-creator-shortcut-coachmark"
			}, {
				trigger: withCtx(() => [renderSlot(_ctx.$slots, "default")]),
				content: withCtx(() => [
					createBaseVNode("div", _hoisted_1, [createBaseVNode("span", _hoisted_2, toDisplayString(unref(i18n).baseText("nodeCreator.shortcutCoachmark.title")), 1)]),
					createBaseVNode("p", _hoisted_3, toDisplayString(unref(i18n).baseText("nodeCreator.shortcutCoachmark.body")), 1),
					createBaseVNode("div", _hoisted_4, [createVNode(unref(N8nButton_default), {
						size: "small",
						label: unref(i18n).baseText("nodeCreator.shortcutCoachmark.gotIt"),
						class: "node-creator-shortcut-coachmark__button",
						onClick: _cache[0] || (_cache[0] = ($event) => emit("dismiss"))
					}, null, 8, ["label"])])
				]),
				_: 3
			}, 8, ["open"]);
		};
	}
});
//#endregion
//#region src/features/shared/nodeCreator/views/NodeCreation.vue?vue&type=script&setup=true&lang.ts
var NodeCreation_vue_vue_type_script_setup_true_lang_default = /* @__PURE__ */ defineComponent({
	__name: "NodeCreation",
	props: {
		nodeViewScale: {},
		createNodeActive: {
			type: Boolean,
			default: false
		},
		focusPanelActive: { type: Boolean }
	},
	emits: [
		"addNodes",
		"toggleNodeCreator",
		"close"
	],
	setup(__props, { emit: __emit }) {
		const LazyNodeCreator = defineAsyncComponent(async () => await __vitePreload(() => import("./NodeCreator-DUXNyRCD.js"), __vite__mapDeps([0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63,64,65,66,67,68,69,70,71,72,73,74,75])));
		const props = __props;
		const emit = __emit;
		const uiStore = useUIStore();
		const focusPanelStore = useFocusPanelStore();
		const setupPanelStore = useSetupPanelStore();
		const i18n = useI18n();
		const telemetry = useTelemetry();
		const assistantStore = useAssistantStore();
		const chatPanelStore = useChatPanelStore();
		const workflowId = useWorkflowId();
		const { getAddedNodesAndConnections } = useActions();
		const { shouldShowCoachmark, onDismissCoachmark } = useNodeCreatorShortcutCoachmark();
		const sidePanelTooltip = computed(() => {
			if (setupPanelStore.isFeatureEnabled) return i18n.baseText("nodeView.openSidePanel");
			return i18n.baseText("nodeView.openFocusPanel");
		});
		function openNodeCreator() {
			emit("toggleNodeCreator", {
				source: NODE_CREATOR_OPEN_SOURCES.ADD_NODE_BUTTON,
				createNodeActive: true
			});
		}
		function addStickyNote() {
			if (document.activeElement) document.activeElement.blur();
			const offset = [...uiStore.nodeViewOffsetPosition];
			const position = getMidCanvasPosition(props.nodeViewScale, offset);
			position[0] -= 240 / 2;
			position[1] -= 160 / 2;
			emit("addNodes", getAddedNodesAndConnections([{
				type: STICKY_NODE_TYPE,
				position
			}]));
		}
		function closeNodeCreator(hasAddedNodes = false) {
			if (props.createNodeActive) emit("toggleNodeCreator", {
				createNodeActive: false,
				hasAddedNodes
			});
			emit("close");
		}
		function nodeTypeSelected(value) {
			emit("addNodes", getAddedNodesAndConnections(value));
			closeNodeCreator(true);
		}
		function toggleFocusPanel() {
			focusPanelStore.toggleFocusPanel();
			telemetry.track(focusPanelStore.focusPanelActive ? "User opened focus panel" : "User closed focus panel", {
				source: "canvasButton",
				parameters: focusPanelStore.focusedNodeParametersInTelemetryFormat
			});
		}
		const { aiAssistant, aiBuilder } = useEditorContext();
		async function onAskAssistantButtonClick() {
			if (aiBuilder.value) await chatPanelStore.toggle({ mode: "builder" });
			else await chatPanelStore.toggle({ mode: "assistant" });
			if (chatPanelStore.isOpen) assistantStore.trackUserOpenedAssistant({
				source: "canvas",
				task: "placeholder",
				has_existing_session: !assistantStore.isSessionEnded,
				workflowId: workflowId.value
			});
		}
		function openCommandBar(event) {
			event.stopPropagation();
			nextTick(() => {
				const keyboardEvent = new KeyboardEvent("keydown", {
					key: "k",
					code: "KeyK",
					metaKey: true,
					bubbles: true,
					cancelable: true
				});
				document.dispatchEvent(keyboardEvent);
			});
		}
		return (_ctx, _cache) => {
			return openBlock(), createElementBlock(Fragment, null, [!__props.createNodeActive ? (openBlock(), createElementBlock("div", {
				key: 0,
				class: normalizeClass(_ctx.$style.nodeButtonsWrapper)
			}, [
				createVNode(NodeCreatorShortcutCoachmark_default, {
					visible: unref(shouldShowCoachmark),
					onDismiss: unref(onDismissCoachmark)
				}, {
					default: withCtx(() => [createVNode(KeyboardShortcutTooltip_default, {
						label: unref(i18n).baseText("nodeView.openNodesPanel"),
						shortcut: { keys: ["N"] },
						placement: "left"
					}, {
						default: withCtx(() => [createVNode(unref(N8nIconButton_default), {
							variant: "subtle",
							size: "large",
							icon: "plus",
							"aria-label": unref(i18n).baseText("nodeView.openNodesPanel"),
							"data-test-id": "node-creator-plus-button",
							onClick: openNodeCreator
						}, null, 8, ["aria-label"])]),
						_: 1
					}, 8, ["label"])]),
					_: 1
				}, 8, ["visible", "onDismiss"]),
				createVNode(KeyboardShortcutTooltip_default, {
					label: unref(i18n).baseText("nodeView.openCommandBar"),
					shortcut: {
						keys: ["k"],
						metaKey: true
					},
					placement: "left"
				}, {
					default: withCtx(() => [createVNode(unref(N8nIconButton_default), {
						variant: "subtle",
						size: "large",
						icon: "search",
						"aria-label": unref(i18n).baseText("nodeView.openCommandBar"),
						"data-test-id": "command-bar-button",
						onClick: openCommandBar
					}, null, 8, ["aria-label"])]),
					_: 1
				}, 8, ["label"]),
				createVNode(KeyboardShortcutTooltip_default, {
					label: unref(i18n).baseText("nodeView.addStickyHint"),
					shortcut: {
						keys: ["s"],
						shiftKey: true
					},
					placement: "left"
				}, {
					default: withCtx(() => [createVNode(unref(N8nIconButton_default), {
						variant: "subtle",
						size: "large",
						icon: "sticky-note",
						"aria-label": unref(i18n).baseText("nodeView.addStickyHint"),
						"data-test-id": "add-sticky-button",
						onClick: addStickyNote
					}, null, 8, ["aria-label"])]),
					_: 1
				}, 8, ["label"]),
				createVNode(KeyboardShortcutTooltip_default, {
					label: sidePanelTooltip.value,
					shortcut: {
						keys: ["f"],
						shiftKey: true
					},
					placement: "left"
				}, {
					default: withCtx(() => [createVNode(unref(N8nIconButton_default), {
						variant: "subtle",
						size: "large",
						icon: "panel-right",
						"aria-label": sidePanelTooltip.value,
						active: __props.focusPanelActive,
						"data-test-id": "toggle-focus-panel-button",
						onClick: toggleFocusPanel
					}, null, 8, ["aria-label", "active"])]),
					_: 1
				}, 8, ["label"]),
				unref(chatPanelStore).isEditableCanvasView && (unref(aiAssistant) || unref(aiBuilder)) ? (openBlock(), createBlock(unref(N8nTooltip_default), {
					key: 0,
					placement: "left"
				}, {
					content: withCtx(() => [createTextVNode(toDisplayString(unref(i18n).baseText("aiAssistant.tooltip")), 1)]),
					default: withCtx(() => [createVNode(unref(N8nButton_default), {
						variant: "subtle",
						iconOnly: "",
						size: "large",
						"aria-label": unref(i18n).baseText("aiAssistant.tooltip"),
						class: normalizeClass(_ctx.$style.icon),
						"data-test-id": "ask-assistant-canvas-action-button",
						onClick: onAskAssistantButtonClick
					}, {
						default: withCtx(() => [createBaseVNode("div", null, [createVNode(unref(AskAssistantIcon_default), { size: "large" })])]),
						_: 1
					}, 8, ["aria-label", "class"])]),
					_: 1
				})) : createCommentVNode("", true)
			], 2)) : createCommentVNode("", true), (openBlock(), createBlock(Suspense, null, {
				default: withCtx(() => [createVNode(unref(LazyNodeCreator), {
					active: __props.createNodeActive,
					onNodeTypeSelected: nodeTypeSelected,
					onCloseNodeCreator: closeNodeCreator
				}, null, 8, ["active"])]),
				_: 1
			}))], 64);
		};
	}
});
var NodeCreation_vue_vue_type_style_index_0_lang_module_default = {
	nodeButtonsWrapper: "_nodeButtonsWrapper_mt5fk_125",
	icon: "_icon_mt5fk_136"
};
var NodeCreation_default = /* @__PURE__ */ _plugin_vue_export_helper_default(NodeCreation_vue_vue_type_script_setup_true_lang_default, [["__cssModules", { "$style": NodeCreation_vue_vue_type_style_index_0_lang_module_default }]]);
//#endregion
export { NodeCreation_default as default };
