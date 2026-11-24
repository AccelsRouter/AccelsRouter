// Baidu's model service (the AIGC endpoints of its VOD product) resells other
// vendors' models: every interface is the channel Base URL followed by the
// vendor's own protocol, with the key as a Bearer token. The plugin sends each
// model family in the format Baidu serves it in, so one channel answers Chat
// Completions, Responses, Claude Messages, Gemini and image requests.
//
// The synchronous interfaces are relay routes, billed by the host as ordinary
// token usage. Baidu's asynchronous video interfaces are not served yet.

const GPT_MODELS = [
  "gpt-5.5",
  "gpt-5.6-luna",
  "gpt-5.6-sol",
  "gpt-5.6-terra",
  "gpt-6-astra",
  "gpt-6-luna",
  "gpt-6-sol",
  "gpt-6.1-sol",
  "gpt-6.1-terra",
  "gpt-6.2-astra",
  "gpt-6.2-sol",
  "gpt-image-2",
  "gpt-image-2.5-flare",
  "gpt-image-2.5-sunburst",
  "gpt-image-2.6-flare",
  "gpt-image-2.6-sunburst",
];
const DUAL_FORMAT_MODELS = [
  "glm-5.1",
  "glm-5.2",
  "glm-5.3",
  "glm-5.3-flash",
  "glm-5.3-flashx",
  "deepseek-v4-pro",
  "deepseek-v4-flash",
  "deepseek-v4-flash-0731",
  "deepseek-v4.1-flash",
  "deepseek-v4.1-flash-1017",
  "kimi-k3",
  "kimi-k3.1pro",
  "qwen3.7-plus",
  "qwen3.8-flash",
  "qwen3.8-max",
  "qwen3.8-max-0902",
];
const CLAUDE_MODELS = [
  "claude-sonnet-5-5",
  "claude-sonnet-5",
  "claude-opus-5",
  "claude-opus-5-5",
  "claude-opus-4-8",
  "claude-sonnet-4-6",
  "claude-sonnet-4-5-20250929",
  "claude-opus-4-7",
  "claude-opus-4-6",
  "claude-opus-4-5-20251101",
  "claude-haiku-5-5",
  "claude-haiku-4-5-20251001",
  "claude-fable-5",
  "claude-fable-5-1",
  "claude-fable-5-2",
];
const GEMINI_MODELS = [
  "gemini-2.5-flash-image",
  "gemini-3-flash-preview",
  "gemini-3-pro-image",
  "gemini-3.1-flash-image",
  "gemini-3.1-flash-lite",
  "gemini-3.1-flash-lite-image",
  "gemini-3.5-flash",
  "gemini-3.5-flash-lite",
  "gemini-3.6-flash",
  "gemini-3.7-flash",
  "gemini-3.8-flash",
  "gemini-4-argon",
  "gemini-4-hydrogen",
  "gemini-nano-banana-2.1",
  "gemini-omni-flash-preview",
];

// Routes pick a family by the prefix of the model name the client sent, so a
// model Baidu adds to a family is served without a plugin update.
// Claude is served in the Claude Messages format only. GLM, DeepSeek, Kimi
// and Qwen are served in the OpenAI and the Claude Messages formats, GPT in
// the OpenAI format. Gemini goes to its native format, which carries image
// data and usage per modality.
const CLAUDE = "re:^claude-";
const GEMINI = "re:^gemini-";
const GPT = "re:^gpt-";
const DUAL_FORMAT = "re:^(glm-|deepseek-|kimi-|qwen)";

// The old system serves the Gemini format under another path prefix than the
// OpenAI format; an empty address is the channel Base URL.
const GEMINI_UPSTREAM = "{config.geminiBaseUrl}/v1beta/models/{model}:generateContent";

export const meta = {
  apiVersion: 1,
  key: "baidu-vod",
  name: { en: "Baidu model service", zh: "百度模型服务" },
  icon: "BaiduCloud.Color",
  description: {
    en: "GPT, Claude, Gemini, GLM, DeepSeek, Kimi and Qwen models through Baidu's model service, in the OpenAI, Claude Messages and Gemini formats",
    zh: "通过百度模型服务调用 GPT、Claude、Gemini、GLM、DeepSeek、Kimi 和 Qwen 模型，支持 OpenAI、Claude Messages 和 Gemini 格式",
  },
  version: "1.0.0",
  author: { name: "AccelsRouter" },
  baseUrl: "https://eos-sg.aigc.exp.bcevod.com",
  models: [...GPT_MODELS, ...DUAL_FORMAT_MODELS, ...CLAUDE_MODELS, ...GEMINI_MODELS],
  channelConfig: {
    type: "object",
    properties: {
      geminiBaseUrl: {
        type: "string",
        format: "url",
        title: { en: "Gemini format address", zh: "Gemini 格式地址" },
        description: {
          en: "Leave empty when the Base URL also serves the Gemini format",
          zh: "Base URL 同时提供 Gemini 格式时留空",
        },
        examples: ["https://vod.bj.baidubce.com/v3/chat/gc"],
      },
    },
  },
  // Without auth a route sends the key as Authorization: Bearer, which Baidu
  // expects in every format. The first route of a path that lists the model
  // serves it; a route without models serves every other model of its path.
  relay: [
    {
      incoming_path: "/v1/chat/completions",
      upstream_path: "/v1/messages",
      converter: "openai_chat_completions_to_anthropic_messages",
      models: [CLAUDE],
    },
    {
      incoming_path: "/v1/chat/completions",
      upstream_path: GEMINI_UPSTREAM,
      converter: "openai_chat_completions_to_gemini_generate_content",
      models: [GEMINI],
    },
    { incoming_path: "/v1/chat/completions", upstream_path: "/v1/chat/completions" },

    // No converter turns a Responses request into Claude Messages, so Claude
    // models are not served on this path.
    {
      incoming_path: "/v1/responses",
      upstream_path: GEMINI_UPSTREAM,
      converter: "openai_responses_to_gemini_generate_content",
      models: [GEMINI],
    },
    { incoming_path: "/v1/responses", upstream_path: "/v1/responses", models: [GPT, DUAL_FORMAT] },

    // No converter turns Claude Messages into the Gemini format, so Gemini
    // models take the OpenAI format here, like GPT.
    { incoming_path: "/v1/messages", upstream_path: "/v1/messages", models: [CLAUDE, DUAL_FORMAT] },
    {
      incoming_path: "/v1/messages",
      upstream_path: "/v1/chat/completions",
      converter: "anthropic_messages_to_openai_chat_completions",
    },

    // A :generateContent path also serves :streamGenerateContent, at the same
    // upstream address, so the stream has its own route ahead of it. No
    // converter turns a Gemini request into Claude Messages, so Claude models
    // are not served on these paths.
    {
      incoming_path: "/v1beta/models/{model}:streamGenerateContent",
      upstream_path: "{config.geminiBaseUrl}/v1beta/models/{model}:streamGenerateContent?alt=sse",
      models: [GEMINI],
    },
    { incoming_path: "/v1beta/models/{model}:generateContent", upstream_path: GEMINI_UPSTREAM, models: [GEMINI] },
    {
      incoming_path: "/v1beta/models/{model}:generateContent",
      upstream_path: "/v1/chat/completions",
      converter: "gemini_generate_content_to_openai_chat_completions",
      models: [GPT, DUAL_FORMAT],
    },

    { incoming_path: "/v1/images/generations", upstream_path: "/v1/images/generations" },
    { incoming_path: "/v1/images/edits", upstream_path: "/v1/images/edits" },
  ],
};

// Runs on the JSON body of every relay request, after the host built it. The
// gpt-image models answer 400 "Unknown parameter: 'response_format'".
export function prepareRequest(_ctx, request) {
  const imageRequest = request.path.startsWith("/v1/images/") && request.model.startsWith("gpt-image");
  if (imageRequest && request.body.response_format !== undefined) {
    return { effects: [{ type: "json.patch", ops: [{ op: "remove", path: "/response_format" }] }] };
  }
  return null;
}
