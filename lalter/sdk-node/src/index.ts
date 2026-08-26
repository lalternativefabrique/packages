export {
  configureLalterClient,
  getLalterClient,
  type LalterClientOptions,
} from "./http-client";

export {
  sendChatMessage,
  ChatRequestError,
  type ChatEvent,
  type ChatEventKind,
  type SendChatMessageInput,
} from "./chat";

export * from "./generated/lalter";
export * as schemas from "./generated/model";
