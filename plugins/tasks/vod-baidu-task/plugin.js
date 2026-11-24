// Fork: the asynchronous video interfaces and the speech synthesis of Baidu's
// model service (the AIGC endpoints of its VOD product), the task part beside
// the provider plugin baidu-vod. Baidu resells other vendors' models: each
// interface is a Baidu address prefix followed by the vendor's own protocol,
// with the API key as a Bearer token. A client calls the gateway at the
// vendor's own path and gets the vendor's own reply.
//
// The key sorts after alibaba, doubao, hailuo, kling and vidu on purpose. The
// host gives a model several plugins declare to the first of them in key order
// for its model-level price labels, and a path several plugins declare to the
// first of them for a request no channel has been chosen for. Those plugins
// therefore keep their models and paths; this one executes only on a channel
// bound to it.
//
// Where a model is also served by a vendor's plugin, the usage facts are that
// plugin's, so the model's price applies to both. Other models are billed per
// output second with the conditions Baidu's price list tells apart.
//
// Addresses, from Baidu's interface documents:
//   Seedance      {new system}/async/seedance + Ark paths
//   Kling 4.0, 3.0 Turbo  {new system}/async/kling + /{endpoint}/{model}
//   Kling, model_name API {Base URL}/v3/aigc/kl + /videos/{endpoint}
//   Vidu          {Base URL}/v3/aigc/vd + Vidu paths
//   PixVerse      {Base URL}/v3/aigc/pv + /video/{endpoint}/generate
//   HappyHorse    {Base URL}/v3/aigc/bailian + DashScope paths
//   MiniMax-H3    {Base URL}/v3/aigc/minimax + /v2 paths
//   Speech        {Base URL}/v2/mini/t2a_v2, signed with an AccessKey

const SECONDS_FIELD = { type: "number", unit: "second", description: { en: "Video generation unit price", zh: "视频生成单价" } };
const RESOLUTION_DESCRIPTION = { en: "Output video resolution", zh: "输出视频分辨率" };

function resolutionField(resolutions) {
  const labels = {};
  for (const resolution of resolutions) labels[resolution] = { en: resolution, zh: resolution };
  return { enum: resolutions, enumLabels: labels, description: RESOLUTION_DESCRIPTION };
}

const VIDEO_INPUT_FIELD = {
  enum: ["none", "video"],
  enumLabels: { none: { en: "No reference video", zh: "无参考视频" }, video: { en: "With reference video", zh: "有参考视频" } },
  description: { en: "Reference video input", zh: "参考视频输入" },
};
const AUDIO_FIELD = {
  enum: ["off", "on"],
  enumLabels: { off: { en: "Silent", zh: "无声" }, on: { en: "With audio", zh: "有声" } },
  description: { en: "Audio output", zh: "音频输出" },
};

// ---------------------------------------------------------------------------
// Seedance. The capabilities and the usage facts of the doubao plugin, whose
// models these are: Ark bills tokens by output resolution and by whether the
// request carries a reference video.
// ---------------------------------------------------------------------------
const SEEDANCE_MODELS = {
  "doubao-seedance-2-0-260128": { resolutions: ["480p", "720p", "1080p", "4k"], maxDuration: 15 },
  "doubao-seedance-2-0-fast-260128": { resolutions: ["480p", "720p"], maxDuration: 15 },
  "doubao-seedance-2-0-mini-260615": { resolutions: ["480p", "720p"], maxDuration: 15 },
  "doubao-seedance-2-5-260628": { resolutions: ["480p", "720p", "1080p"], maxDuration: 30 },
};
const SEEDANCE_RESOLUTIONS = ["480p", "720p", "1080p", "4k"];
// A channel may map a declared model to a name outside the table.
const DEFAULT_SEEDANCE_MODEL = { resolutions: SEEDANCE_RESOLUTIONS, maxDuration: 30 };

function seedanceUsageSchema(profile) {
  return {
    // Ark's billing tokens: estimated at submit, reported on completion.
    tokens: { type: "number", unit: "token", description: { en: "Billing token unit price", zh: "计费 Token 单价" } },
    resolution: resolutionField(profile.resolutions),
    video_input: VIDEO_INPUT_FIELD,
  };
}

// Ark's formula: tokens = seconds x width x height x 24 / 1024, at the widest
// 16:9 frame of the resolution so that the estimate is not below the bill.
const SEEDANCE_FRAMES = { "480p": [854, 480], "720p": [1280, 720], "1080p": [1920, 1080], "4k": [3840, 2160] };

function seedanceTokens(seconds, resolution) {
  const frame = SEEDANCE_FRAMES[resolution] || SEEDANCE_FRAMES["720p"];
  return (seconds * frame[0] * frame[1] * 24) / 1024;
}

function seedanceUsageExamples(profile) {
  const examples = profile.resolutions.map((resolution) => ({
    label: resolution + " · 5s",
    facts: { tokens: Math.round(seedanceTokens(5, resolution)), resolution: resolution, video_input: "none" },
  }));
  examples.push({ label: "720p · 5s (+4s 输入视频)", facts: { tokens: Math.round(seedanceTokens(9, "720p")), resolution: "720p", video_input: "video" } });
  return examples;
}

// ---------------------------------------------------------------------------
// Kling. Baidu serves Kling's two API families: the one that takes the model
// in the path (Kling 4.0, 4.0 Flash and 3.0 Turbo here) and the earlier one
// that takes model_name in the body.
// ---------------------------------------------------------------------------
const KLING_PATH_MODELS = {
  "kling-4.0": { endpoints: ["text-to-video", "image-to-video", "omni-video"], resolutions: ["720p", "1080p"], maxSeconds: 30 },
  "kling-4.0-flash": { endpoints: ["text-to-video", "image-to-video", "omni-video"], resolutions: ["720p", "1080p"], maxSeconds: 30, totalDuration: true },
  "kling-3.0-turbo": { endpoints: ["text-to-video", "image-to-video"], resolutions: ["720p", "1080p"], maxSeconds: 15 },
};
// kling-v1, which Baidu also lists, is left out: an upstream test of the gateway
// (controller/task_plugin_test.go) registers a plugin whose model is KLING-V1,
// and the host refuses two plugins whose model names differ only in case.
const KLING_BODY_MODELS = [
  "kling-v1-5",
  "kling-v2",
  "kling-v2-master",
  "kling-v2-1",
  "kling-v2-1-master",
  "kling-v2-5-turbo",
  "kling-v2-6",
  "kling-v3",
  "kling-v3-omni",
  "kling-video-o1",
];
const KLING_BODY_ENDPOINTS = ["text2video", "image2video", "omni-video"];
const KLING_PATH_ACTIONS = { "text-to-video": "text_to_video", "image-to-video": "image_to_video", "omni-video": "reference_to_video" };
const KLING_BODY_ACTIONS = { text2video: "text_to_video", image2video: "image_to_video", "omni-video": "reference_to_video" };

// The models the kling plugin also serves are billed as it bills them: in
// Kling's resource-package units, of which Kling's price page gives the
// consumption per output second (the kling plugin's tables), settled on the
// units Kling reports. Kling 4.0 bills a total duration: the output, or with
// an input video twice the output, or input plus output when the input is
// longer.
const KLING_UNIT_MODELS = {
  "kling-v2-5-turbo": { units: { std: 0.3, pro: 0.5 } },
  "kling-v2-6": { units: { std: 0.3, pro: 0.5 }, soundUnits: { pro: 1.0 }, voiceUnits: { pro: 1.2 } },
  "kling-v3": { units: { std: 0.6, pro: 0.8, "4k": 3.0 }, soundUnits: { std: 0.9, pro: 1.2, "4k": 3.0 } },
};
const KLING_DEAREST_UNITS = { units: { std: 0.9, pro: 1.2, "4k": 3.0 }, soundUnits: { std: 0.9, pro: 1.2, "4k": 3.0 }, voiceUnits: { std: 1.2, pro: 1.2, "4k": 3.0 } };
const KLING4_UNITS = { "720p": 1.0, "1080p": 1.5 };
const KLING4_VIDEO_INPUT_UNITS = { "720p": 0.6, "1080p": 0.9 };
const KLING_UNITS_SCHEMA = { units: { type: "number", unit: "credit", description: { en: "Kling credit unit price", zh: "可灵资源包单位单价" } } };
// Every other Kling model is billed per output second by resolution, audio and
// reference video, the conditions of Baidu's Kling price list. The model_name
// API names the resolution by mode.
const KLING_SECONDS_SCHEMA = { seconds: SECONDS_FIELD, resolution: resolutionField(["720p", "1080p", "4k"]), audio: AUDIO_FIELD, video_input: VIDEO_INPUT_FIELD };
const KLING_MODE_RESOLUTIONS = { std: "720p", pro: "1080p", "4k": "4k" };
const KLING_PATH_MIN_SECONDS = 3;
// Whether an element is a video character is not in the request, so an element
// counts as an input video.
const KLING_VIDEO_INPUTS = ["feature_video", "base_video", "element", "video"];
// Kling would call these back with its own task ids, which the gateway does
// not hand out.
const KLING_UNSUPPORTED_FIELDS = ["callback_url", "external_task_id"];

function klingBilledInUnits(model) {
  return model === "kling-4.0" || Object.prototype.hasOwnProperty.call(KLING_UNIT_MODELS, model);
}

// ---------------------------------------------------------------------------
// Vidu. The endpoints, durations (shortest, longest, default) and resolutions
// (the default first) of the vidu plugin, whose usage facts these models keep.
// ---------------------------------------------------------------------------
const VIDU_Q4 = { durations: [3, 16, 5], resolutions: ["720p", "540p", "1080p", "2K", "4K"] };
const VIDU_Q3 = { durations: [1, 16, 5], resolutions: ["720p", "540p", "1080p"] };
const VIDU_Q3_FAST = { durations: [1, 16, 5], resolutions: ["720p", "1080p"] };
const VIDU_Q3_REFERENCE = { durations: [3, 16, 5], resolutions: ["720p", "540p", "1080p"] };
const VIDU_Q3_SCENE = { durations: [3, 15, 5], resolutions: ["720p", "1080p"] };
const VIDU_Q2 = { durations: [1, 10, 5], resolutions: ["720p", "540p", "1080p"] };
const VIDU_Q2_FRAMES = { durations: [1, 10, 5], resolutions: ["720p", "360p", "540p", "1080p"] };
const VIDU_Q2_FAST = { durations: [1, 10, 5], resolutions: ["720p", "1080p"] };
const VIDU_Q1 = { durations: [5, 5, 5], resolutions: ["1080p"] };
const VIDU_2 = { durations: [4, 8, 4], resolutions: ["360p", "720p", "1080p"], longest: ["720p"] };
const VIDU_ENDPOINTS = {
  text2video: { action: "text_to_video", models: { "viduq3-pro": VIDU_Q3, "viduq3-turbo": VIDU_Q3, viduq1: VIDU_Q1 } },
  img2video: {
    action: "image_to_video",
    models: {
      viduq4: VIDU_Q4,
      "viduq3-pro": VIDU_Q3,
      "viduq3-turbo": VIDU_Q3,
      "viduq3-pro-fast": VIDU_Q3_FAST,
      "viduq2-pro": VIDU_Q2,
      "viduq2-turbo": VIDU_Q2,
      "viduq1-classic": VIDU_Q1,
      viduq1: VIDU_Q1,
      "vidu2.0": VIDU_2,
    },
  },
  "start-end2video": {
    action: "first_tail_to_video",
    models: {
      "viduq3-pro": VIDU_Q3,
      "viduq3-turbo": VIDU_Q3,
      "viduq3-pro-fast": VIDU_Q3_FAST,
      "viduq2-pro": VIDU_Q2_FRAMES,
      "viduq2-turbo": VIDU_Q2_FRAMES,
      "viduq2-pro-fast": VIDU_Q2_FAST,
      viduq1: VIDU_Q1,
      "viduq1-classic": VIDU_Q1,
      "vidu2.0": VIDU_2,
    },
  },
  reference2video: {
    action: "reference_to_video",
    models: {
      viduq4: VIDU_Q4,
      viduq3: VIDU_Q3_REFERENCE,
      "viduq3-turbo": VIDU_Q3_REFERENCE,
      "viduq3-mix": VIDU_Q3_SCENE,
      "viduq3-ad": VIDU_Q3_SCENE,
      "viduq3-drama": { durations: [2, 16, 8], resolutions: ["1080p", "720p"] },
      "viduq2-pro": VIDU_Q2,
      viduq1: VIDU_Q1,
      "vidu2.0": { durations: [4, 4, 4], resolutions: ["360p", "720p"] },
    },
  },
};
const VIDU_MODELS = [
  "vidu2.0",
  "viduq1-classic",
  "viduq1",
  "viduq2-turbo",
  "viduq2-pro",
  "viduq2-pro-fast",
  "viduq3",
  "viduq3-pro",
  "viduq3-turbo",
  "viduq3-pro-fast",
  "viduq3-mix",
  "viduq3-ad",
  "viduq3-drama",
  "viduq4",
];
// Vidu's resolutions from the cheapest to the dearest.
const VIDU_RESOLUTIONS = ["360p", "540p", "720p", "1080p", "2K", "4K"];
const VIDU_USAGE_SCHEMA = {
  // The credits Vidu reports for the finished task.
  credits: { type: "number", unit: "credit", description: { en: "Vidu credit unit price", zh: "Vidu 积分单价" } },
  duration: SECONDS_FIELD,
  resolution: resolutionField(VIDU_RESOLUTIONS),
};

// ---------------------------------------------------------------------------
// PixVerse. Baidu's model list names the models pixverse3.5 ... pixversev6.0;
// PixVerse's own body names them v3.5 ... v6, and takes either here.
// ---------------------------------------------------------------------------
const PIXVERSE_MODELS = {
  "pixverse3.5": "v3.5",
  "pixverse4.0": "v4",
  "pixverse4.5": "v4.5",
  "pixverse5.0": "v5",
  "pixverse5.5": "v5.5",
  "pixversev5.6": "v5.6",
  "pixversev6.0": "v6",
};
const PIXVERSE_ENDPOINTS = { text: "text_to_video", img: "image_to_video", transition: "first_tail_to_video" };
const PIXVERSE_QUALITIES = ["360p", "540p", "720p", "1080p"];
const PIXVERSE_MAX_SECONDS = 15;
const PIXVERSE_USAGE_SCHEMA = {
  seconds: SECONDS_FIELD,
  resolution: resolutionField(PIXVERSE_QUALITIES),
  motion_mode: {
    enum: ["normal", "fast"],
    enumLabels: { normal: { en: "Normal", zh: "标准模式" }, fast: { en: "Fast", zh: "流畅模式" } },
    description: { en: "Motion mode", zh: "运动模式" },
  },
  audio: AUDIO_FIELD,
  multi_clip: { type: "boolean", description: { en: "Whether multi-clip generation is enabled", zh: "是否开启多片段生成" } },
};

// ---------------------------------------------------------------------------
// HappyHorse, on Bailian's DashScope protocol: billed per output second at 720P
// or 1080P.
// ---------------------------------------------------------------------------
const HAPPYHORSE_MODELS = [
  "happyhorse-1.0-t2v",
  "happyhorse-1.0-i2v",
  "happyhorse-1.0-r2v",
  "happyhorse-1.0-video-edit",
  "happyhorse-1.1-t2v",
  "happyhorse-1.1-i2v",
  "happyhorse-1.1-r2v",
];
// The longest output reserved for a request that leaves the duration out, as
// video editing does; the duration Bailian reports settles the task.
const HAPPYHORSE_MAX_SECONDS = 15;
const HAPPYHORSE_USAGE_SCHEMA = { seconds: SECONDS_FIELD, resolution: resolutionField(["720P", "1080P"]) };

// ---------------------------------------------------------------------------
// MiniMax-H3 and MiniMax-H3-MAX, on MiniMax's /v2 video API. H3 keeps the
// usage facts of the hailuo plugin; Baidu bills H3-MAX for its output alone.
// ---------------------------------------------------------------------------
const H3_MODEL = "MiniMax-H3";
const H3_MAX_MODEL = "MiniMax-H3-MAX";
const H3_MIN_DURATION = 4;
const H3_MAX_DURATION = 15;
const H3_MAX_REFERENCE_IMAGES = 9;
const H3_MAX_INPUT_VIDEO_SECONDS = 15;
const H3_USAGE_SCHEMA = {
  seconds: SECONDS_FIELD,
  resolution: { enum: ["768P", "2K"], description: RESOLUTION_DESCRIPTION },
  input_images: { type: "number", unit: "count", description: { en: "Input image unit price", zh: "输入图片单价" } },
  input_video_seconds: { type: "number", unit: "second", description: { en: "Input video unit price", zh: "输入视频单价" } },
};
const H3_MAX_USAGE_SCHEMA = { seconds: SECONDS_FIELD, resolution: resolutionField(["480P", "768P"]) };

// ---------------------------------------------------------------------------
// Speech synthesis: MiniMax's t2a_v2, billed per character, a Chinese
// character counting two.
// ---------------------------------------------------------------------------
const SPEECH_MODELS = ["speech-2.8-hd", "speech-2.8-turbo", "speech-2.6-hd", "speech-2.6-turbo", "speech-02-hd", "speech-02-turbo", "speech-01-hd", "speech-01-turbo"];
const SPEECH_MAX_CHARACTERS = 10000;
const SPEECH_USAGE_SCHEMA = {
  characters: { type: "number", unit: "token", description: { en: "Speech synthesis unit price", zh: "语音合成单价" } },
};

// One submit route per model and endpoint of the Kling API that takes the
// model in its path; the kling plugin declares the Kling 4.0 paths the same way.
const klingPathRoutes = [];
for (const model of Object.keys(KLING_PATH_MODELS)) {
  for (const endpoint of KLING_PATH_MODELS[model].endpoints) {
    klingPathRoutes.push({ method: "POST", path: "/" + endpoint + "/" + model, model: model, endpoint: endpoint, decode: "kling_" + (endpoint + "_" + model).replace(/[^a-z0-9]/g, "_") });
  }
}

export const meta = {
  apiVersion: 1,
  key: "vod-baidu-task",
  name: { en: "Baidu model service (video and speech)", zh: "百度模型服务（视频与语音）" },
  icon: "BaiduCloud.Color",
  description: {
    en: "Seedance, Kling, Vidu, PixVerse, HappyHorse and MiniMax video generation and MiniMax speech synthesis through Baidu's model service, in each vendor's own format",
    zh: "通过百度模型服务调用 Seedance、可灵、Vidu、PixVerse、HappyHorse、MiniMax 视频生成与 MiniMax 语音合成，使用各厂商自己的接口格式",
  },
  version: "1.0.0",
  author: { name: "AccelsRouter" },
  baseUrl: "https://vod.bj.baidubce.com",
  allowedHosts: ["vod.gz.baidubce.com"],
  keyHint: {
    en: "API Key, or API Key|AccessKey|SecretKey to also serve speech synthesis",
    zh: "API Key；需要语音合成时填 API Key|AccessKey|SecretKey",
  },
  channelConfig: {
    type: "object",
    properties: {
      asyncBaseUrl: {
        type: "string",
        format: "url",
        title: { en: "New system address", zh: "新系统地址" },
        description: { en: "Serves Seedance, Kling 4.0 and Kling 3.0 Turbo", zh: "用于 Seedance、可灵 4.0 与可灵 3.0 Turbo" },
        default: "https://vod.gz.baidubce.com",
      },
    },
  },
  models: Object.keys(SEEDANCE_MODELS).concat(Object.keys(KLING_PATH_MODELS), KLING_BODY_MODELS, VIDU_MODELS, Object.keys(PIXVERSE_MODELS), HAPPYHORSE_MODELS, [H3_MODEL, H3_MAX_MODEL], SPEECH_MODELS),
  fetchMode: "per_task",
  // The fallback of a channel alias that resolves to several models.
  usageSchema: { seconds: SECONDS_FIELD },
  usageProfiles: Object.keys(SEEDANCE_MODELS)
    .map((model) => ({ models: [model], schema: seedanceUsageSchema(SEEDANCE_MODELS[model]), examples: seedanceUsageExamples(SEEDANCE_MODELS[model]) }))
    .concat([
      { models: Object.keys(KLING_PATH_MODELS).concat(KLING_BODY_MODELS).filter(klingBilledInUnits), schema: KLING_UNITS_SCHEMA },
      { models: Object.keys(KLING_PATH_MODELS).concat(KLING_BODY_MODELS).filter((model) => !klingBilledInUnits(model)), schema: KLING_SECONDS_SCHEMA },
      { models: VIDU_MODELS, schema: VIDU_USAGE_SCHEMA },
      { models: Object.keys(PIXVERSE_MODELS), schema: PIXVERSE_USAGE_SCHEMA },
      { models: HAPPYHORSE_MODELS, schema: HAPPYHORSE_USAGE_SCHEMA },
      { models: [H3_MODEL], schema: H3_USAGE_SCHEMA },
      { models: [H3_MAX_MODEL], schema: H3_MAX_USAGE_SCHEMA },
      { models: SPEECH_MODELS, schema: SPEECH_USAGE_SCHEMA, examples: [{ label: "1000 字符", facts: { characters: 1000 } }] },
    ]),
  // Each vendor's own paths, which the vendor's plugin declares too where
  // there is one; the gateway also serves each under /vod-baidu-task.
  routes: [
    { method: "POST", path: "/api/v3/contents/generations/tasks", type: "submit", decode: "createSeedanceTask", render: "seedanceCreated" },
    { method: "GET", path: "/api/v3/contents/generations/tasks/:task_id", type: "query", render: "seedanceStatus" },
  ]
    .concat(klingPathRoutes.map((route) => ({ method: route.method, path: route.path, type: "submit", decode: route.decode, render: "klingPathCreated" })))
    .concat([{ method: "GET", path: "/tasks", type: "query", taskIdParam: "task_ids", render: "klingPathTasks" }])
    .concat(KLING_BODY_ENDPOINTS.map((endpoint) => ({ method: "POST", path: "/v1/videos/" + endpoint, type: "submit", decode: "createKlingVideo", render: "klingCreated" })))
    .concat(KLING_BODY_ENDPOINTS.map((endpoint) => ({ method: "GET", path: "/v1/videos/" + endpoint + "/:task_id", type: "query", render: "klingStatus" })))
    .concat(Object.keys(VIDU_ENDPOINTS).map((endpoint) => ({ method: "POST", path: "/ent/v2/" + endpoint, type: "submit", decode: "createViduTask", render: "viduCreated" })))
    .concat([{ method: "GET", path: "/ent/v2/tasks/:task_id/creations", type: "query", render: "viduStatus" }])
    // PixVerse's own paths. Extending a video, lip sync and fusion are not served.
    .concat(Object.keys(PIXVERSE_ENDPOINTS).map((endpoint) => ({ method: "POST", path: "/openapi/v2/video/" + endpoint + "/generate", type: "submit", decode: "createPixverseVideo", render: "pixverseCreated" })))
    .concat([
      { method: "GET", path: "/openapi/v2/video/result/:task_id", type: "query", render: "pixverseStatus" },
      { method: "POST", path: "/api/v1/services/aigc/video-generation/video-synthesis", type: "submit", decode: "createHappyHorseTask", render: "happyHorseCreated" },
      { method: "GET", path: "/api/v1/tasks/:task_id", type: "query", render: "happyHorseStatus" },
      { method: "POST", path: "/v2/video_generation", type: "submit", decode: "createH3Task", render: "h3Created" },
      { method: "GET", path: "/v2/query/video_generation/:task_id", type: "query", render: "h3Status" },
      // MiniMax answers with the audio; there is no task to query again.
      { method: "POST", path: "/v1/t2a_v2", type: "submit", decode: "createSpeech", render: "speechCreated", retainResult: false },
    ]),
};

function trimmed(value) {
  return String(value || "").trim();
}

function plainObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

function jsonBody(ctx) {
  if (!ctx.body || ctx.body.kind !== "json") throw new Error("JSON body required");
  const body = ctx.body.value;
  if (!body || typeof body !== "object" || Array.isArray(body)) throw new Error("request body must be an object");
  return body;
}

function lastSegment(path, skip) {
  const segments = String(path || "").split("/").filter(Boolean);
  return segments[segments.length - 1 - (skip || 0)] || "";
}

// Durations an upstream reports are bounded before they become usage.
function reportedSeconds(value) {
  const seconds = Number.parseFloat(value);
  return Number.isFinite(seconds) && seconds > 0 ? Math.min(seconds, 3600) : 0;
}

// The channel key is the API key, optionally followed by an AccessKey and its
// SecretKey for the interfaces Baidu serves with a signature only.
function keyParts(ctx) {
  const parts = trimmed(ctx.apiKey).split("|").map(trimmed);
  if (!parts[0]) throw new Error("api_key is empty");
  return parts;
}

function bearerHeaders(ctx) {
  return { "Content-Type": "application/json", Accept: "application/json", Authorization: "Bearer " + keyParts(ctx)[0] };
}

function asyncBase(ctx) {
  return trimmed(plainObject(ctx.config).asyncBaseUrl) || ctx.baseUrl;
}

function upstreamModel(ctx) {
  return trimmed(ctx.upstreamModel) || trimmed(ctx.model) || trimmed(plainObject(ctx.requestBody).model);
}

// ------------------------------- Seedance ---------------------------------

function seedanceProfile(ctx) {
  for (const model of [ctx.upstreamModel, ctx.model].map(trimmed)) {
    if (Object.prototype.hasOwnProperty.call(SEEDANCE_MODELS, model)) return SEEDANCE_MODELS[model];
  }
  return DEFAULT_SEEDANCE_MODEL;
}

function seedanceHasVideo(content) {
  return Array.isArray(content) && content.some((item) => item && (item.type === "video_url" || Object.prototype.hasOwnProperty.call(item, "video_url")));
}

// The resolution the reservation is estimated at: the requested one, which the
// model must offer, or the model's highest up to 1080p when Ark picks it.
function seedanceResolution(ctx, body) {
  const profile = seedanceProfile(ctx);
  const raw = trimmed(body.resolution).toLowerCase();
  if (!SEEDANCE_RESOLUTIONS.includes(raw)) return profile.resolutions.includes("1080p") ? "1080p" : profile.resolutions[profile.resolutions.length - 1];
  if (!profile.resolutions.includes(raw)) throw new Error(upstreamModel(ctx) + " resolution must be one of " + profile.resolutions.join(", "));
  return raw;
}

function seedanceUsage(ctx, body) {
  const profile = seedanceProfile(ctx);
  // A missing duration or -1, with which the model picks the length, reserves
  // the longest the model makes.
  let seconds = typeof body.duration === "number" || typeof body.duration === "string" ? Number(body.duration) : 0;
  if (!Number.isFinite(seconds) || seconds <= 0) {
    const frames = Number(body.frames);
    seconds = Number.isFinite(frames) && frames > 0 ? Math.floor(frames / 24) : profile.maxDuration;
  }
  if (seconds <= 0) seconds = 5;
  seconds = Math.min(seconds, 3600);
  const resolution = seedanceResolution(ctx, body);
  return { tokens: seedanceTokens(seconds, resolution), resolution: resolution, video_input: seedanceHasVideo(body.content) ? "video" : "none" };
}

// --------------------------------- Kling -----------------------------------

function klingDuration(value, shortest, longest) {
  const n = typeof value === "string" && value.trim() !== "" ? Number(value) : value;
  if (!Number.isInteger(n) || n < shortest || n > longest) throw new Error("duration must be an integer from " + shortest + " to " + longest);
  return n;
}

function klingPathResolution(profile, settings) {
  const resolution = trimmed(settings.resolution).toLowerCase();
  return profile.resolutions.includes(resolution) ? resolution : profile.resolutions[profile.resolutions.length - 1];
}

function klingPathHasVideo(body) {
  return (Array.isArray(body.contents) ? body.contents : []).some((item) => item && KLING_VIDEO_INPUTS.includes(item.type));
}

// The seconds Kling bills a request of the API that takes the model in its
// path: the output, or for the models that bill a total duration with an input
// video, twice the output or input plus output. Without a duration Kling picks
// the length, and only Kling knows the length of an input video, so the longest
// of each is reserved.
function klingPathBilledSeconds(profile, body, withTotal) {
  const settings = plainObject(body.settings);
  const open = settings.duration === undefined || settings.duration === null;
  const seconds = open ? profile.maxSeconds : klingDuration(settings.duration, KLING_PATH_MIN_SECONDS, profile.maxSeconds);
  if (!withTotal || !klingPathHasVideo(body)) return seconds;
  return Math.max(seconds * 2, profile.maxSeconds + seconds);
}

// The usage of a Kling request: units for the models the kling plugin also
// serves, seconds with their conditions for the others.
function klingUsage(ctx) {
  const req = ctx.requestBody;
  const body = req.vendorBody;
  const model = upstreamModel(ctx);
  if (req.api === "path") {
    const profile = KLING_PATH_MODELS[req.model];
    const settings = plainObject(body.settings);
    const resolution = klingPathResolution(profile, settings);
    if (klingBilledInUnits(req.model)) {
      const billed = klingPathBilledSeconds(profile, body, true);
      return { units: (klingPathHasVideo(body) ? KLING4_VIDEO_INPUT_UNITS : KLING4_UNITS)[resolution] * billed };
    }
    return {
      seconds: klingPathBilledSeconds(profile, body, profile.totalDuration === true),
      resolution: resolution,
      audio: trimmed(settings.audio).toLowerCase() === "off" ? "off" : "on",
      video_input: klingPathHasVideo(body) ? "video" : "none",
    };
  }
  const seconds = body.duration === undefined || body.duration === null ? 5 : klingDuration(body.duration, 1, 3600);
  const mode = trimmed(body.mode).toLowerCase() || "std";
  const sound = trimmed(body.sound).toLowerCase() === "on";
  if (klingBilledInUnits(req.model)) {
    const profile = Object.prototype.hasOwnProperty.call(KLING_UNIT_MODELS, model) ? KLING_UNIT_MODELS[model] : KLING_DEAREST_UNITS;
    let table = profile.units;
    if (sound) {
      const voice = Array.isArray(body.voice_list) && body.voice_list.length > 0;
      table = (voice && profile.voiceUnits) || profile.soundUnits || (voice ? KLING_DEAREST_UNITS.voiceUnits : KLING_DEAREST_UNITS.soundUnits);
    }
    const rate = Object.prototype.hasOwnProperty.call(table, mode) ? table[mode] : Object.keys(table).reduce((dearest, key) => Math.max(dearest, table[key]), 0);
    return { units: rate * seconds };
  }
  return {
    seconds: seconds,
    resolution: KLING_MODE_RESOLUTIONS[mode] || "4k",
    audio: sound ? "on" : "off",
    video_input: Array.isArray(body.video_list) && body.video_list.length > 0 ? "video" : "none",
  };
}

// The task's entry in a reply of the API that takes the model in its path: the
// list GET /tasks answers, or the single task of the create reply.
function klingPathEntry(reply, taskId) {
  const data = plainObject(reply).data;
  if (!Array.isArray(data)) return plainObject(data);
  return data.find((entry) => entry && taskId && entry.id === taskId) || plainObject(data[0]);
}

function klingPathVideos(entry) {
  return (Array.isArray(entry.outputs) ? entry.outputs : []).filter((output) => output && output.type === "video");
}

// The usage of a finished task of the API that takes the model in its path.
function klingPathCompleted(ctx, body) {
  const state = plainObject(ctx.state);
  const entry = klingPathEntry(body, ctx.taskId);
  if (entry.status !== "succeeded") return null;
  const videos = klingPathVideos(entry);
  const output = reportedSeconds(videos.length ? videos[0].duration : "");
  const input = reportedSeconds(plainObject(entry.usage).input_duration);
  if (state.units) {
    // A cash deduction counts its list price, not the discounted amount.
    const deductions = Array.isArray(entry.billing) ? entry.billing : [];
    const deducted = deductions.reduce((sum, item) => {
      const type = item && item.charge_type;
      const units = Number.parseFloat(type === "unit" ? item.amount : type === "cash" ? item.list_price : "");
      return units >= 0 ? sum + units : Number.NaN;
    }, 0);
    if (deductions.length && Number.isFinite(deducted)) return { units: deducted };
    if (!output) return null;
    if (!input) return { units: KLING4_UNITS[state.resolution] * output };
    return { units: KLING4_VIDEO_INPUT_UNITS[state.resolution] * (input <= output ? output * 2 : input + output) };
  }
  // The reservation is the most the request could cost, so a longer duration
  // in the reply is not believed.
  const facts = Object.assign({}, plainObject(state.facts));
  const reserved = facts.seconds;
  const produced = Math.ceil(output) || (state.open ? 0 : state.requested);
  if (state.total && facts.video_input === "video") {
    if (produced && input) facts.seconds = Math.min(reserved, Math.ceil(input <= produced ? produced * 2 : input + produced));
  } else if (state.open && produced) {
    facts.seconds = Math.min(reserved, produced);
  }
  return facts;
}

// ---------------------------------- Vidu -----------------------------------

function viduProfile(endpoint, model) {
  const models = VIDU_ENDPOINTS[endpoint].models;
  return Object.prototype.hasOwnProperty.call(models, model) ? models[model] : null;
}

// The duration and resolution a Vidu request is reserved for, as the vidu
// plugin reserves a native request: those read from the body, the model's
// defaults on the endpoint for those left out, and otherwise the dearest the
// model renders.
function viduUsage(endpoint, model, body) {
  const listed = viduProfile(endpoint, model);
  let profiles = [listed];
  if (!listed) profiles = Object.keys(VIDU_ENDPOINTS).map((name) => viduProfile(name, model)).filter(Boolean);
  if (!profiles.length) {
    for (const name of Object.keys(VIDU_ENDPOINTS)) profiles = profiles.concat(Object.keys(VIDU_ENDPOINTS[name].models).map((key) => VIDU_ENDPOINTS[name].models[key]));
  }
  const settings = { duration: 0, resolution: VIDU_RESOLUTIONS[0] };
  for (const profile of profiles) {
    settings.duration = Math.max(settings.duration, profile.durations[1]);
    for (const tier of profile.resolutions) {
      if (VIDU_RESOLUTIONS.indexOf(tier) > VIDU_RESOLUTIONS.indexOf(settings.resolution)) settings.resolution = tier;
    }
  }
  const seconds = typeof body.duration === "number" || typeof body.duration === "string" ? Number(body.duration) : NaN;
  let durationKnown = true;
  if (Number.isInteger(seconds) && seconds > 0 && seconds <= 3600) settings.duration = seconds;
  else if ((body.duration === undefined || body.duration === null) && listed) settings.duration = listed.durations[2];
  else durationKnown = false;
  const tier = VIDU_RESOLUTIONS.find((item) => item.toLowerCase() === trimmed(body.resolution).toLowerCase());
  if (tier) settings.resolution = tier;
  else if ((body.resolution === undefined || body.resolution === null) && listed && durationKnown)
    settings.resolution = listed.longest && settings.duration === listed.durations[1] ? listed.longest[0] : listed.resolutions[0];
  return settings;
}

// -------------------------------- PixVerse ---------------------------------

function pixverseUsage(body) {
  const seconds = typeof body.duration === "string" && body.duration.trim() !== "" ? Number(body.duration) : body.duration;
  if (!Number.isInteger(seconds) || seconds < 1 || seconds > PIXVERSE_MAX_SECONDS) throw new Error("duration must be an integer from 1 to " + PIXVERSE_MAX_SECONDS);
  const quality = trimmed(body.quality).toLowerCase();
  if (!PIXVERSE_QUALITIES.includes(quality)) throw new Error("quality must be one of " + PIXVERSE_QUALITIES.join(", "));
  return {
    seconds: seconds,
    resolution: quality,
    motion_mode: trimmed(body.motion_mode).toLowerCase() === "fast" ? "fast" : "normal",
    audio: body.generate_audio_switch === true ? "on" : "off",
    multi_clip: body.generate_multi_clip_switch === true,
  };
}

// Baidu's task detail, the only query its PixVerse interface has: the video is
// the source of the first media the task produced, wherever the detail holds it.
function taskDetailVideoURL(value, depth) {
  if (!value || typeof value !== "object" || depth > 8) return "";
  if (Array.isArray(value.mediaBasicInfos)) {
    for (const media of value.mediaBasicInfos) {
      const url = trimmed(plainObject(plainObject(media).source).sourceUrl);
      if (url) return url;
    }
  }
  for (const key of Object.keys(value)) {
    const url = taskDetailVideoURL(value[key], depth + 1);
    if (url) return url;
  }
  return "";
}

function taskDetailResult(body) {
  const statuses = { READY: "QUEUED", RUNNING: "IN_PROGRESS", SUCCESS: "SUCCESS", FAILED: "FAILURE" };
  const status = statuses[trimmed(body.status).toUpperCase()];
  if (!status) return { status: "UNKNOWN", reason: "unrecognized status: " + trimmed(body.status) };
  const result = { status: status, progress: status === "QUEUED" ? "10%" : status === "IN_PROGRESS" ? "50%" : "100%" };
  if (status === "SUCCESS") {
    const url = taskDetailVideoURL(body, 0);
    if (url) result.url = url;
  }
  if (status === "FAILURE") {
    const error = plainObject(body.error);
    result.reason = trimmed(body.errorMessage) || trimmed(body.errMsg) || trimmed(error.message) || trimmed(body.message) || "task failed";
  }
  return result;
}

// ------------------------------- HappyHorse --------------------------------

function happyHorseUsage(body) {
  const parameters = plainObject(body.parameters);
  let seconds = HAPPYHORSE_MAX_SECONDS;
  if (parameters.duration !== undefined && parameters.duration !== null) {
    seconds = typeof parameters.duration === "string" && parameters.duration.trim() !== "" ? Number(parameters.duration) : parameters.duration;
    if (!Number.isInteger(seconds) || seconds < 1 || seconds > 3600) throw new Error("parameters.duration must be an integer from 1 to 3600");
  }
  return { seconds: seconds, resolution: trimmed(parameters.resolution).toUpperCase() === "720P" ? "720P" : "1080P" };
}

// ------------------------------- MiniMax-H3 --------------------------------

function h3Duration(model, value) {
  const shortest = model === H3_MODEL ? H3_MIN_DURATION : 1;
  const seconds = typeof value === "string" && value.trim() !== "" ? Number(value) : value;
  if (!Number.isInteger(seconds) || seconds < shortest || seconds > H3_MAX_DURATION) throw new Error(model + " duration must be an integer between " + shortest + " and " + H3_MAX_DURATION + " seconds");
  return seconds;
}

// A duration or resolution the body leaves out or the plugin cannot read is
// reserved at the dearest.
function h3Usage(model, body) {
  const missing = body.duration === undefined || body.duration === null || body.duration === "";
  const seconds = missing ? H3_MAX_DURATION : h3Duration(model, body.duration);
  const resolution = trimmed(body.resolution).toUpperCase();
  if (model !== H3_MODEL) return { seconds: seconds, resolution: resolution === "480P" ? "480P" : "768P" };
  const content = Array.isArray(body.content) ? body.content : [];
  return {
    seconds: seconds,
    resolution: resolution === "768P" ? "768P" : "2K",
    input_images: content.filter((item) => item && item.type === "image_url").length,
    // The request does not tell the length of an input video: the longest is
    // reserved, and the length MiniMax reports settles the task.
    input_video_seconds: content.some((item) => item && item.type === "video_url") ? H3_MAX_INPUT_VIDEO_SECONDS : 0,
  };
}

function h3Task(body) {
  const task = plainObject(body).task;
  return task && typeof task === "object" && !Array.isArray(task) ? task : null;
}

// --------------------------------- Speech ----------------------------------

// The characters MiniMax bills: a Chinese character counts two, any other one.
function speechCharacters(text) {
  let count = 0;
  for (const character of text) {
    const code = character.codePointAt(0);
    const chinese = (code >= 0x3400 && code <= 0x4dbf) || (code >= 0x4e00 && code <= 0x9fff) || (code >= 0xf900 && code <= 0xfaff) || (code >= 0x20000 && code <= 0x3134f);
    count += chinese ? 2 : 1;
  }
  return count;
}

function bceEncode(value) {
  return encodeURIComponent(value).replace(/[!'()*]/g, (character) => "%" + character.charCodeAt(0).toString(16).toUpperCase());
}

// Baidu Cloud's request signature (bce-auth-v1) over the method, the path and
// the host and x-bce-date headers, for an address without a query.
function bceHeaders(method, url, accessKey, secretKey) {
  const address = /^https?:\/\/([^/?#]+)([^?#]*)/.exec(url);
  if (!address) throw new Error("invalid upstream address");
  const timestamp = new Date(utils.unixNow() * 1000).toISOString().replace(/\.\d+Z$/, "Z");
  const prefix = "bce-auth-v1/" + accessKey + "/" + timestamp + "/1800";
  const signingKey = utils.hmacSHA256(prefix, secretKey);
  const canonical = [method, address[2].split("/").map(bceEncode).join("/") || "/", "", "host:" + bceEncode(address[1].toLowerCase()) + "\nx-bce-date:" + bceEncode(timestamp)].join("\n");
  return { "x-bce-date": timestamp, Authorization: prefix + "/host;x-bce-date/" + utils.hmacSHA256(canonical, signingKey) };
}

// ------------------------------ native routes ------------------------------

// The reply a status route answers with before the first poll and after the
// gateway failed a task the vendor still reports as running.
function withTaskId(data, name, id) {
  const output = Object.assign({}, plainObject(data));
  output[name] = id;
  return output;
}

export const native = {
  // Ark's own body, sent as is for Ark to check. Ark would call callback_url
  // back with its own task ids, and a draft task names an earlier task by one.
  createSeedanceTask: function (ctx) {
    const body = jsonBody(ctx);
    const model = trimmed(body.model);
    if (!model) throw new Error("model is required");
    if (body.callback_url !== undefined && body.callback_url !== null) throw new Error("callback_url is not supported; query the task instead");
    if (!Array.isArray(body.content) || !body.content.some((item) => item && typeof item === "object" && !Array.isArray(item))) throw new Error("content is required");
    if (body.content.some((item) => item && item.type === "draft_task")) throw new Error("draft_task content is not supported");
    const hasReference = body.content.some((item) => item && typeof item === "object" && item.type !== "text");
    return { kind: "submit", model: model, action: hasReference ? "image_to_video" : "text_to_video", requestBody: { vendor: "seedance", model: model, vendorBody: body } };
  },
  seedanceCreated: function (ctx, task) {
    return withTaskId(task.data, "id", task.task_id);
  },
  seedanceStatus: function (ctx, task) {
    const statuses = { NOT_START: "queued", SUBMITTED: "queued", QUEUED: "queued", IN_PROGRESS: "running", SUCCESS: "succeeded", FAILURE: "failed" };
    const output = withTaskId(task.data, "id", task.task_id);
    if (typeof output.status !== "string") output.status = statuses[task.status] || "queued";
    if (task.status === "FAILURE" && (output.status === "queued" || output.status === "running")) {
      output.status = "failed";
      output.error = { code: "InternalServiceError", message: task.fail_reason || "task failed" };
    }
    return output;
  },

  klingPathCreated: function (ctx, task) {
    const result = plainObject(task.data);
    return Object.assign({}, result, { data: withTaskId(result.data, "id", task.task_id) });
  },
  // Kling answers GET /tasks with a list; the gateway answers for one task.
  klingPathTasks: function (ctx, task) {
    const statuses = { NOT_START: "submitted", SUBMITTED: "submitted", QUEUED: "submitted", IN_PROGRESS: "processing", SUCCESS: "succeeded", FAILURE: "failed" };
    const result = plainObject(task.data);
    const entry = Object.assign({ status: statuses[task.status] || "submitted", create_time: task.created_at * 1000, update_time: (task.updated_at || task.created_at) * 1000 }, klingPathEntry(result), {
      id: task.task_id,
    });
    if (task.status === "FAILURE" && entry.status !== "failed") {
      entry.status = "failed";
      entry.message = task.fail_reason || "";
    }
    return Object.assign({ code: 0, message: "", request_id: "" }, result, { data: [entry] });
  },
  // Kling's own body, whose model_name is required, sent as is for Kling to
  // check. A duration is billed, so it is bounded first.
  createKlingVideo: function (ctx) {
    const body = jsonBody(ctx);
    const model = trimmed(body.model_name);
    if (!model) throw new Error("model_name is required");
    if (Object.prototype.hasOwnProperty.call(KLING_PATH_MODELS, model)) throw new Error(model + " is served at /text-to-video/" + model + " and the other paths that take the model");
    for (const name of KLING_UNSUPPORTED_FIELDS) {
      if (body[name] !== undefined && body[name] !== null) throw new Error(name + " is not supported; query the task instead");
    }
    if (body.duration !== undefined && body.duration !== null) klingDuration(body.duration, 1, 3600);
    const endpoint = lastSegment(ctx.path, 0);
    return { kind: "submit", model: model, action: KLING_BODY_ACTIONS[endpoint], requestBody: { vendor: "kling", api: "body", model: model, endpoint: endpoint, vendorBody: body } };
  },
  klingCreated: function (ctx, task) {
    const result = plainObject(task.data);
    return Object.assign({}, result, { data: withTaskId(result.data, "task_id", task.task_id) });
  },
  klingStatus: function (ctx, task) {
    const statuses = { NOT_START: "submitted", SUBMITTED: "submitted", QUEUED: "submitted", IN_PROGRESS: "processing", SUCCESS: "succeed", FAILURE: "failed" };
    const result = plainObject(task.data);
    const data = withTaskId(result.data, "task_id", task.task_id);
    if (typeof data.task_status !== "string") data.task_status = statuses[task.status] || "submitted";
    if (task.status === "FAILURE" && data.task_status !== "failed") {
      data.task_status = "failed";
      data.task_status_msg = task.fail_reason || "";
    }
    return Object.assign({ code: 0, message: "", request_id: "" }, result, { data: data });
  },

  // Vidu's own body, sent as is for Vidu to check. The route names the endpoint.
  createViduTask: function (ctx) {
    const body = jsonBody(ctx);
    const model = trimmed(body.model);
    if (!model) throw new Error("model is required");
    if (body.callback_url !== undefined && body.callback_url !== null) throw new Error("callback_url is not supported; query the task instead");
    const endpoint = lastSegment(ctx.path, 0);
    return { kind: "submit", model: model, action: VIDU_ENDPOINTS[endpoint].action, requestBody: { vendor: "vidu", model: model, endpoint: endpoint, vendorBody: body } };
  },
  viduCreated: function (ctx, task) {
    return withTaskId(task.data, "task_id", task.task_id);
  },
  // Vidu's query reply. Before the first poll the task holds the create reply.
  viduStatus: function (ctx, task) {
    const data = plainObject(task.data);
    let output = withTaskId(data, "id", task.task_id);
    if (data.task_id !== undefined && data.id === undefined) output = { id: task.task_id, state: task.status === "IN_PROGRESS" ? "processing" : "created", err_code: "", creations: [] };
    if (task.status === "FAILURE" && output.state !== "failed") {
      output.state = "failed";
      output.err_code = output.err_code || task.fail_reason || "task failed";
    }
    return output;
  },

  // PixVerse's own body. The model, the duration and the quality set the
  // price, so the plugin reads them before PixVerse checks the rest.
  createPixverseVideo: function (ctx) {
    const body = jsonBody(ctx);
    const named = trimmed(body.model);
    const model = Object.keys(PIXVERSE_MODELS).find((name) => name === named || PIXVERSE_MODELS[name] === named.toLowerCase());
    if (!model) throw new Error("model must be one of " + Object.keys(PIXVERSE_MODELS).map((name) => PIXVERSE_MODELS[name]).join(", "));
    pixverseUsage(body);
    const endpoint = lastSegment(ctx.path, 1);
    return { kind: "submit", model: model, action: PIXVERSE_ENDPOINTS[endpoint], requestBody: { vendor: "pixverse", model: model, endpoint: endpoint, vendorBody: body } };
  },
  pixverseCreated: function (ctx, task) {
    return { ErrCode: 0, ErrMsg: "success", Resp: { video_id: task.task_id } };
  },
  // PixVerse's result reply: status 1 is a finished video, 5 one in the
  // making and 8 a failed one.
  pixverseStatus: function (ctx, task) {
    const resp = { id: task.task_id, status: task.status === "SUCCESS" ? 1 : task.status === "FAILURE" ? 8 : 5 };
    if (task.status === "SUCCESS") resp.url = taskDetailVideoURL(task.data, 0);
    return { ErrCode: 0, ErrMsg: task.status === "FAILURE" ? task.fail_reason || "task failed" : "success", Resp: resp };
  },

  // Bailian's own body, sent as is for Bailian to check.
  createHappyHorseTask: function (ctx) {
    const body = jsonBody(ctx);
    const model = trimmed(body.model);
    if (!model) throw new Error("model is required");
    if (!body.input || typeof body.input !== "object" || Array.isArray(body.input)) throw new Error("input is required");
    happyHorseUsage(body);
    return { kind: "submit", model: model, action: model.endsWith("-t2v") ? "text_to_video" : "image_to_video", requestBody: { vendor: "happyhorse", model: model, vendorBody: body } };
  },
  happyHorseCreated: function (ctx, task) {
    const result = plainObject(task.data);
    return Object.assign({}, result, { output: withTaskId(result.output, "task_id", task.task_id) });
  },
  happyHorseStatus: function (ctx, task) {
    const statuses = { NOT_START: "PENDING", SUBMITTED: "PENDING", QUEUED: "PENDING", IN_PROGRESS: "RUNNING", SUCCESS: "SUCCEEDED", FAILURE: "FAILED" };
    const result = plainObject(task.data);
    const output = withTaskId(result.output, "task_id", task.task_id);
    if (typeof output.task_status !== "string") output.task_status = statuses[task.status] || "PENDING";
    if (task.status === "FAILURE" && output.task_status !== "FAILED" && output.task_status !== "CANCELED") {
      output.task_status = "FAILED";
      output.message = task.fail_reason || "task failed";
    }
    return Object.assign({}, result, { output: output });
  },

  // MiniMax-H3's own body, sent as is for MiniMax to check.
  createH3Task: function (ctx) {
    const body = jsonBody(ctx);
    const model = trimmed(body.model);
    if (!model) throw new Error("model is required");
    if (body.callback_url !== undefined && body.callback_url !== null) throw new Error("callback_url is not supported; query the task instead");
    if (!Array.isArray(body.content)) throw new Error("content must be an array");
    if (body.duration !== undefined && body.duration !== null && body.duration !== "") h3Duration(model === H3_MODEL ? H3_MODEL : H3_MAX_MODEL, body.duration);
    const visual = body.content.some((item) => item && (item.type === "image_url" || item.type === "video_url"));
    return { kind: "submit", model: model, action: visual ? "image_to_video" : "text_to_video", requestBody: { vendor: "minimax", model: model, vendorBody: body } };
  },
  h3Created: function (ctx, task) {
    return withTaskId(task.data, "task_id", task.task_id);
  },
  // The /v2 query reply, {task: {...}}. Before the first poll the task holds
  // the create reply, which is not part of it.
  h3Status: function (ctx, task) {
    const data = plainObject(task.data);
    const current = h3Task(data);
    const output = Object.assign({}, current || {}, { id: task.task_id });
    if (task.status === "FAILURE" && output.status !== "failed" && output.status !== "cancelled") {
      output.status = "failed";
      output.error = { message: task.fail_reason || "task failed" };
    } else if (!current) {
      output.status = task.status === "IN_PROGRESS" ? "processing" : "queued";
    }
    return current ? Object.assign({}, data, { task: output }) : { task: output };
  },

  // MiniMax's own t2a_v2 body. The text is billed, so its length is bounded
  // first; MiniMax checks the rest.
  createSpeech: function (ctx) {
    const body = jsonBody(ctx);
    const model = trimmed(body.model);
    if (!model) throw new Error("model is required");
    if (typeof body.text !== "string" || !body.text.trim()) throw new Error("text is required");
    if (speechCharacters(body.text) > SPEECH_MAX_CHARACTERS) throw new Error("text must be at most " + SPEECH_MAX_CHARACTERS + " characters, a Chinese character counting two");
    if (body.stream === true) throw new Error("stream is not supported; the audio is returned once it is synthesized");
    return { kind: "submit", model: model, action: "text_to_speech", requestBody: { vendor: "speech", model: model, vendorBody: body } };
  },
  speechCreated: function (ctx, task) {
    return plainObject(task.data);
  },

  // Each vendor's own error reply, by the path the client called.
  error: function (ctx, error) {
    const path = String(ctx.path || "");
    if (path.indexOf("/api/v3/") >= 0) return { error: { code: error.httpStatus === 400 ? "InvalidParameter" : "InternalServiceError", message: error.message } };
    if (path.indexOf("/openapi/v2/") >= 0) return { ErrCode: error.httpStatus, ErrMsg: error.message };
    if (path.indexOf("/api/v1/") >= 0) return { code: error.code, message: error.message, request_id: error.requestId || "" };
    if (path.indexOf("/ent/v2/") >= 0) return { code: error.code, message: error.message };
    if (path.indexOf("/v2/") >= 0) return { type: "error", error: { type: error.httpStatus >= 500 ? "server_error" : "bad_request_error", message: error.message, http_code: String(error.httpStatus) }, request_id: error.requestId || "" };
    if (path.indexOf("/t2a_v2") >= 0) return { __status: 200, __body: { base_resp: { status_code: error.httpStatus >= 500 ? 1013 : 2013, status_msg: error.message } } };
    return { code: error.httpStatus >= 500 ? 5000 : 1200, message: error.message, request_id: error.requestId || "" };
  },
};

// The decoders of the Kling paths that take the model: the route names the
// model and the endpoint. Kling would call back, or look tasks up, by ids the
// gateway does not hand out; the duration is billed, so it is bounded.
for (const route of klingPathRoutes) {
  native[route.decode] = function (ctx) {
    const body = jsonBody(ctx);
    const options = plainObject(body.options);
    for (const name of KLING_UNSUPPORTED_FIELDS) {
      if (options[name] !== undefined && options[name] !== null && options[name] !== "") throw new Error("options." + name + " is not supported; query the task instead");
    }
    const settings = plainObject(body.settings);
    if (settings.duration !== undefined && settings.duration !== null) klingDuration(settings.duration, KLING_PATH_MIN_SECONDS, KLING_PATH_MODELS[route.model].maxSeconds);
    return {
      kind: "submit",
      model: route.model,
      action: KLING_PATH_ACTIONS[route.endpoint],
      requestBody: { vendor: "kling", api: "path", model: route.model, endpoint: route.endpoint, vendorBody: body },
    };
  };
}

// ------------------------------- driver hooks -------------------------------

// A request reaches Baidu as the client sent it, with the channel's model.
export function buildSubmitRequest(ctx) {
  const req = ctx.requestBody;
  const model = upstreamModel(ctx);
  const descriptor = { method: "POST", headers: bearerHeaders(ctx), action: ctx.action };
  // Reject what cannot be billed before any quota is reserved.
  extractUsage(ctx);
  if (req.vendor === "seedance") {
    descriptor.url = asyncBase(ctx) + "/async/seedance/api/v3/contents/generations/tasks";
    descriptor.body = Object.assign({}, req.vendorBody, { model: model });
  } else if (req.vendor === "kling" && req.api === "path") {
    descriptor.url = asyncBase(ctx) + "/async/kling/" + req.endpoint + "/" + model;
    descriptor.body = req.vendorBody;
  } else if (req.vendor === "kling") {
    // Without this header Baidu answers in its own structure instead of Kling's.
    descriptor.headers["X-Response-Format"] = "orion";
    descriptor.url = ctx.baseUrl + "/v3/aigc/kl/videos/" + req.endpoint;
    descriptor.body = Object.assign({}, req.vendorBody, { model_name: model });
  } else if (req.vendor === "vidu") {
    descriptor.url = ctx.baseUrl + "/v3/aigc/vd/ent/v2/" + req.endpoint;
    descriptor.body = Object.assign({}, req.vendorBody, { model: model });
  } else if (req.vendor === "pixverse") {
    descriptor.url = ctx.baseUrl + "/v3/aigc/pv/video/" + req.endpoint + "/generate";
    descriptor.body = Object.assign({}, req.vendorBody, { model: PIXVERSE_MODELS[model] || model });
  } else if (req.vendor === "happyhorse") {
    descriptor.headers["X-DashScope-Async"] = "enable";
    descriptor.url = ctx.baseUrl + "/v3/aigc/bailian/api/v1/services/aigc/video-generation/video-synthesis";
    descriptor.body = Object.assign({}, req.vendorBody, { model: model });
  } else if (req.vendor === "minimax") {
    if (model !== H3_MODEL && model !== H3_MAX_MODEL) throw new Error(model + " is not served on /v2/video_generation");
    descriptor.url = ctx.baseUrl + "/v3/aigc/minimax/v2/video_generation";
    descriptor.body = Object.assign({}, req.vendorBody, { model: model });
  } else if (req.vendor === "speech") {
    const parts = keyParts(ctx);
    if (parts.length !== 3 || !parts[1] || !parts[2]) throw new Error("speech synthesis needs a channel key of the form API Key|AccessKey|SecretKey");
    descriptor.url = ctx.baseUrl + "/v2/mini/t2a_v2";
    descriptor.headers = Object.assign({ "Content-Type": "application/json", Accept: "application/json" }, bceHeaders("POST", descriptor.url, parts[1], parts[2]));
    descriptor.body = Object.assign({}, req.vendorBody, { model: model });
  } else {
    throw new Error("unsupported request");
  }
  return descriptor;
}

// The plugin state names the vendor for the polls, which have no request, and
// keeps what settles a task whose reply does not report its usage.
export function parseSubmitResponse(ctx, resp) {
  const req = ctx.requestBody;
  const body = plainObject(resp.body);
  const state = { vendor: req.vendor };
  let taskId = "";
  if (req.vendor === "seedance") {
    taskId = trimmed(body.id);
  } else if (req.vendor === "kling" && req.api === "path") {
    if (body.code !== 0) throw new Error(trimmed(body.message) || "kling submit failed");
    taskId = trimmed(plainObject(body.data).id);
    const profile = KLING_PATH_MODELS[req.model];
    const settings = plainObject(req.vendorBody.settings);
    state.api = "path";
    state.resolution = klingPathResolution(profile, settings);
    if (klingBilledInUnits(req.model)) state.units = true;
    else {
      state.facts = klingUsage(ctx);
      state.open = settings.duration === undefined || settings.duration === null;
      state.requested = state.open ? 0 : Number(settings.duration);
      state.total = profile.totalDuration === true;
    }
  } else if (req.vendor === "kling") {
    if (body.code !== undefined && body.code !== 0 && body.code !== "0") throw new Error(trimmed(body.message) || "kling submit failed");
    taskId = trimmed(plainObject(body.data).task_id);
    state.endpoint = req.endpoint;
    if (klingBilledInUnits(req.model)) state.units = true;
    // Baidu's own structure, {taskId}, is the reply to a request it did not
    // read as Kling's. The task exists and is billed, so it is followed
    // through Baidu's task detail and settles at the reservation.
    if (!taskId && trimmed(body.taskId)) {
      taskId = trimmed(body.taskId);
      state.detail = true;
    }
  } else if (req.vendor === "vidu") {
    if (body.state === "failed") throw new Error("task failed");
    taskId = trimmed(body.task_id);
  } else if (req.vendor === "pixverse") {
    taskId = trimmed(body.taskId);
    if (!taskId && (body.code || body.message)) throw new Error(trimmed(body.code) + ": " + trimmed(body.message));
  } else if (req.vendor === "happyhorse") {
    if (body.code) throw new Error(body.code + ": " + trimmed(body.message));
    taskId = trimmed(plainObject(body.output).task_id);
  } else if (req.vendor === "minimax") {
    const error = plainObject(body.error);
    if (trimmed(error.message)) throw new Error(error.message);
    const base = plainObject(body.base_resp);
    if (base.status_code) throw new Error(trimmed(base.status_msg) || "minimax submit failed");
    taskId = trimmed(body.task_id) || trimmed(plainObject(body.task).id);
  } else if (req.vendor === "speech") {
    const base = plainObject(body.base_resp);
    if (base.status_code) throw new Error(trimmed(base.status_msg) || "speech synthesis failed");
    if (body.code || !trimmed(plainObject(body.data).audio)) throw new Error(trimmed(body.message) || "speech synthesis returned no audio");
    if (!trimmed(ctx.publicTaskId)) throw new Error("missing gateway task id");
    return { taskId: trimmed(ctx.publicTaskId), taskData: body, state: state, immediate: { status: "SUCCESS", progress: "100%" } };
  }
  if (!taskId) throw new Error("missing task id");
  return { taskId: taskId, taskData: body, state: state };
}

export function extractUsage(ctx) {
  if (ctx.usagePurpose === "billing_ratios") return null;
  const req = ctx.requestBody;
  const body = req.vendorBody;
  if (req.vendor === "seedance") return seedanceUsage(ctx, body);
  if (req.vendor === "kling") return klingUsage(ctx);
  if (req.vendor === "vidu") return viduUsage(req.endpoint, upstreamModel(ctx), body);
  if (req.vendor === "pixverse") return pixverseUsage(body);
  if (req.vendor === "happyhorse") return happyHorseUsage(body);
  if (req.vendor === "minimax") return h3Usage(upstreamModel(ctx), body);
  if (req.vendor === "speech") {
    const characters = speechCharacters(body.text);
    if (characters > SPEECH_MAX_CHARACTERS) throw new Error("text must be at most " + SPEECH_MAX_CHARACTERS + " characters, a Chinese character counting two");
    return { characters: characters };
  }
  throw new Error("unsupported request");
}

export function buildQueryRequest(ctx) {
  const state = plainObject(ctx.state);
  const id = encodeURIComponent(ctx.taskId);
  let url;
  if (state.vendor === "seedance") url = asyncBase(ctx) + "/async/seedance/api/v3/contents/generations/tasks/" + id;
  else if (state.vendor === "kling" && state.api === "path") url = asyncBase(ctx) + "/async/kling/tasks?task_ids=" + id;
  else if (state.vendor === "kling") url = ctx.baseUrl + "/v3/aigc/kl/videos/" + state.endpoint + "/" + id;
  else if (state.vendor === "vidu") url = ctx.baseUrl + "/v3/aigc/vd/ent/v2/tasks/" + id + "/creations";
  // Baidu's PixVerse interface has no query of its own.
  else if (state.vendor === "pixverse" || state.detail) url = ctx.baseUrl + "/v3/tasks/" + id;
  else if (state.vendor === "happyhorse") url = ctx.baseUrl + "/v3/aigc/bailian/api/v1/tasks/" + id;
  else if (state.vendor === "minimax") url = ctx.baseUrl + "/v3/aigc/minimax/v2/query/video_generation/" + id;
  else throw new Error("the task has no status to query");
  return { url: url, method: "GET", headers: { Accept: "application/json", Authorization: "Bearer " + keyParts(ctx)[0] } };
}

export function parseTaskResult(ctx, body) {
  const state = plainObject(ctx.state);
  body = plainObject(body);
  if (state.detail) return taskDetailResult(body);
  if (state.vendor === "seedance") {
    if (body.status === "queued") return { status: "QUEUED", progress: "10%" };
    if (body.status === "running") return { status: "IN_PROGRESS", progress: "50%" };
    if (body.status === "succeeded") return { status: "SUCCESS", progress: "100%", url: trimmed(plainObject(body.content).video_url) };
    if (body.status === "failed" || body.status === "expired" || body.status === "cancelled")
      return { status: "FAILURE", progress: "100%", reason: trimmed(plainObject(body.error).message) || body.status };
    return { status: "UNKNOWN", reason: "unrecognized status: " + trimmed(body.status) };
  }
  if (state.vendor === "kling" && state.api === "path") {
    const entry = klingPathEntry(body, ctx.taskId);
    const statuses = { submitted: "SUBMITTED", processing: "IN_PROGRESS", succeeded: "SUCCESS", failed: "FAILURE" };
    const status = statuses[entry.status];
    if (!status) return { status: "UNKNOWN", reason: "unknown task status: " + trimmed(entry.status) };
    const result = { status: status, reason: trimmed(entry.message) };
    const videos = status === "SUCCESS" ? klingPathVideos(entry) : [];
    if (videos.length && videos[0].url) result.url = videos[0].url;
    return result;
  }
  if (state.vendor === "kling") {
    const data = plainObject(body.data);
    const statuses = { submitted: "SUBMITTED", processing: "IN_PROGRESS", succeed: "SUCCESS", failed: "FAILURE" };
    const status = statuses[data.task_status];
    if (!status) return { status: "UNKNOWN", reason: "unknown task status: " + trimmed(data.task_status) };
    const result = { status: status, reason: trimmed(data.task_status_msg) };
    const videos = status === "SUCCESS" ? plainObject(data.task_result).videos : null;
    if (Array.isArray(videos) && videos.length && videos[0].url) result.url = videos[0].url;
    return result;
  }
  if (state.vendor === "vidu") {
    const statuses = { created: "SUBMITTED", queueing: "SUBMITTED", processing: "IN_PROGRESS", success: "SUCCESS", failed: "FAILURE" };
    const status = statuses[body.state];
    if (!status) return { status: "UNKNOWN", reason: "unknown task state: " + trimmed(body.state) };
    const result = { status: status, reason: body.state === "failed" ? trimmed(body.err_code) : "" };
    const url = Array.isArray(body.creations) && body.creations.length ? trimmed(plainObject(body.creations[0]).url) : "";
    if (url) result.url = url;
    return result;
  }
  if (state.vendor === "pixverse") return taskDetailResult(body);
  if (state.vendor === "happyhorse") {
    const output = plainObject(body.output);
    if (output.task_status === "PENDING") return { status: "QUEUED" };
    if (output.task_status === "RUNNING") return { status: "IN_PROGRESS" };
    if (output.task_status === "SUCCEEDED") return { status: "SUCCESS", url: trimmed(output.video_url) };
    if (["FAILED", "CANCELED", "UNKNOWN"].includes(output.task_status)) return { status: "FAILURE", reason: trimmed(body.message) || trimmed(output.message) || "task failed" };
    return { status: "UNKNOWN", reason: "unrecognized status: " + trimmed(output.task_status) };
  }
  if (state.vendor === "minimax") {
    const task = h3Task(body);
    // Until a task is finished Baidu may answer with its own task status
    // instead of MiniMax's reply.
    if (!task) return body.status ? taskDetailResult(body) : { status: "UNKNOWN", reason: "unrecognized reply" };
    const statuses = { queued: "QUEUED", running: "IN_PROGRESS", processing: "IN_PROGRESS", succeeded: "SUCCESS", failed: "FAILURE", cancelled: "FAILURE" };
    const status = statuses[task.status];
    if (!status) return { status: "UNKNOWN", reason: "unrecognized status: " + trimmed(task.status) };
    const result = { status: status, progress: status === "QUEUED" ? "30%" : status === "IN_PROGRESS" ? "50%" : "100%" };
    if (status === "SUCCESS") result.url = trimmed(plainObject(task.content).url);
    if (status === "FAILURE") result.reason = trimmed(plainObject(task.error).message) || "task " + trimmed(task.status);
    return result;
  }
  return { status: "UNKNOWN", reason: "the task has no vendor" };
}

// The usage the vendor reports for a finished task replaces the estimate; a
// value it leaves out, or one outside what the request could cost, keeps it.
export function extractUsageOnComplete(ctx, _result, body) {
  const state = plainObject(ctx.state);
  body = plainObject(body);
  if (state.detail) return null;
  if (state.vendor === "seedance") {
    if (body.status !== "succeeded") return {};
    const facts = {};
    const usage = plainObject(body.usage);
    let tokens = Number(usage.completion_tokens);
    if (!Number.isFinite(tokens) || tokens <= 0) tokens = Number(usage.total_tokens);
    if (Number.isFinite(tokens) && tokens > 0) facts.tokens = tokens;
    const resolution = trimmed(plainObject(body.content).resolution || body.resolution).toLowerCase();
    if (seedanceProfile(ctx).resolutions.includes(resolution)) facts.resolution = resolution;
    return facts;
  }
  if (state.vendor === "kling" && state.api === "path") return klingPathCompleted(ctx, body);
  if (state.vendor === "kling") {
    if (!state.units) return null;
    const deduction = plainObject(body.data).final_unit_deduction;
    if (deduction === undefined || deduction === null || deduction === "") return null;
    const units = Number.parseFloat(deduction);
    return Number.isFinite(units) && units >= 0 ? { units: units } : null;
  }
  if (state.vendor === "vidu") {
    if (body.credits === undefined || body.credits === null) return null;
    const credits = Number(body.credits);
    return Number.isFinite(credits) && credits >= 0 ? { credits: credits } : null;
  }
  if (state.vendor === "happyhorse") {
    const output = plainObject(body.output);
    const usage = plainObject(body.usage);
    const facts = {};
    const seconds = reportedSeconds(usage.output_video_duration ?? usage.duration ?? usage.video_duration);
    if (seconds) facts.seconds = seconds;
    const resolution = trimmed(usage.SR ?? output.resolution).toUpperCase().replace(/^(\d+)$/, "$1P");
    if (resolution === "720P" || resolution === "1080P") facts.resolution = resolution;
    return facts;
  }
  if (state.vendor === "minimax") {
    const task = h3Task(body);
    if (!task) return null;
    const max = trimmed(ctx.upstreamModel || ctx.model) !== H3_MODEL;
    const usage = plainObject(task.usage);
    const facts = {};
    const resolution = trimmed(task.resolution).toUpperCase();
    if ((max ? ["480P", "768P"] : ["768P", "2K"]).includes(resolution)) facts.resolution = resolution;
    const fields = [{ key: "seconds", value: usage.output_seconds, minimum: 1, maximum: H3_MAX_DURATION, integer: false }];
    if (!max) {
      fields.push({ key: "input_images", value: usage.input_image_count, minimum: 0, maximum: H3_MAX_REFERENCE_IMAGES, integer: true });
      fields.push({ key: "input_video_seconds", value: usage.input_seconds, minimum: 0, maximum: H3_MAX_INPUT_VIDEO_SECONDS, integer: false });
    }
    for (const field of fields) {
      if (field.value === undefined || field.value === null || field.value === "") continue;
      const value = Number(field.value);
      if (!Number.isFinite(value) || value < field.minimum || value > field.maximum || (field.integer && !Number.isInteger(value))) continue;
      facts[field.key] = value;
    }
    return Object.keys(facts).length ? facts : null;
  }
  if (state.vendor === "speech") {
    const characters = plainObject(body.extra_info).usage_characters;
    return Number.isInteger(characters) && characters >= 1 && characters <= SPEECH_MAX_CHARACTERS ? { characters: characters } : null;
  }
  return null;
}

// A key is the API key alone, or the API key with an AccessKey and its
// SecretKey.
export function checkKey(_ctx, key) {
  const parts = String(key || "").split("|").map(trimmed);
  if ((parts.length === 1 || parts.length === 3) && parts.every(Boolean)) return;
  return { error: { en: "The key must be an API Key, or API Key|AccessKey|SecretKey", zh: "密钥应为 API Key，或 API Key|AccessKey|SecretKey" } };
}
