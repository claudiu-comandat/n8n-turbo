import { Fi as MODAL_CLOSE, Ii as MODAL_CONFIRM, Pi as MODAL_CANCEL, pi as AGENT_CONFIRMATION_MODAL_KEY } from "./constants-CqN-rIyX.js";
import { _ as useUIStore } from "./users.store-Cqhh8Mpv.js";
//#region src/features/agents/composables/useAgentConfirmationModal.ts
function useAgentConfirmationModal() {
	const uiStore = useUIStore();
	async function openAgentConfirmationModal(options) {
		return await new Promise((resolve) => {
			uiStore.openModalWithData({
				name: AGENT_CONFIRMATION_MODAL_KEY,
				data: {
					...options,
					onConfirm: () => {
						resolve(MODAL_CONFIRM);
					},
					onCancel: () => {
						resolve(MODAL_CANCEL);
					},
					onClose: () => {
						resolve(MODAL_CLOSE);
					}
				}
			});
		});
	}
	return { openAgentConfirmationModal };
}
//#endregion
export { useAgentConfirmationModal as t };
