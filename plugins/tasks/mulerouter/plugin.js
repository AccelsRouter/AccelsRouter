// Fork: MuleRouter (api.mulerouter.ai) unified Type-61 plugin covering 14
// asynchronous video/super-resolution/image generation models and 1 OpenAI-
// compatible text model (carrothub-papaya-1.0).

const MAX_INPUT_IMAGE_BYTES = 20 * 1024 * 1024;
const MAX_LORA_ITEMS = 3;
const MAX_SEED = 2147483647;
const BERRY_RATIOS = ["adaptive", "16:9", "4:3", "1:1", "3:4", "9:16"];

const VIDEO_MODEL_SPECS = {
  "berry-1.0": {
    kind: "berry",
    vendor: "carrothub",
    resolutions: ["480p", "720p", "1080p"],
    defaultResolution: "1080p",
    minDuration: 2,
    maxDuration: 30,
    defaultDuration: 5,
    allowsAutoDuration: true,
    referenceVideoMaxReserve: 15,
  },
  "berry-1.0-pro": {
    kind: "berry",
    vendor: "carrothub",
    resolutions: ["1080p", "2k", "4k"],
    defaultResolution: "1080p",
    minDuration: 2,
    maxDuration: 30,
    defaultDuration: 5,
    allowsAutoDuration: true,
    referenceVideoMaxReserve: 15,
  },
  "berry-1.0-turbo": {
    kind: "berry",
    vendor: "carrothub",
    resolutions: ["480p", "720p", "1080p"],
    defaultResolution: "1080p",
    minDuration: 2,
    maxDuration: 30,
    defaultDuration: 5,
    allowsAutoDuration: true,
    referenceVideoMaxReserve: 15,
  },
  "berry-1.0-pro-turbo": {
    kind: "berry",
    vendor: "carrothub",
    resolutions: ["1080p", "2k", "4k"],
    defaultResolution: "1080p",
    minDuration: 2,
    maxDuration: 30,
    defaultDuration: 5,
    allowsAutoDuration: true,
    referenceVideoMaxReserve: 15,
  },
  "wan2.7-i2v-spicy": {
    kind: "wan27",
    vendor: "carrothub",
    resolutions: ["720p", "1080p"],
    defaultResolution: "1080p",
    minDuration: 2,
    maxDuration: 15,
    defaultDuration: 5,
  },
  "wan2.2-i2v-spicy": {
    kind: "wan22",
    vendor: "carrothub",
    resolutions: ["480p", "720p"],
    defaultResolution: "480p",
    allowedDurations: [5, 8],
    defaultDuration: 5,
  },
  "w2.2-i2v-lora": {
    kind: "lora",
    vendor: "carrothub",
    resolutions: ["480p", "720p"],
    defaultResolution: "480p",
    allowedDurations: [5, 8],
    defaultDuration: 5,
  },
  "wan-animate": {
    kind: "animate",
    vendor: "carrothub",
    resolutions: ["480p", "720p"],
    defaultResolution: "720p",
    maxDuration: 30,
  },
  flashvsr: {
    kind: "flashvsr",
    vendor: "mulerouter",
    resolutions: ["720p", "1080p", "2k", "4k"],
    maxDuration: 120,
  },
};

const IMAGE_MODEL_SPECS = {
  "z-image-spicy": {
    kind: "zimage",
    vendor: "carrothub",
    minDim: 256,
    maxDim: 1536,
    defaultWidth: 1024,
    defaultHeight: 1536,
    defaultPromptExtend: true,
  },
  "z-image-spicy-pro": {
    kind: "zimage_pro",
    vendor: "carrothub",
    minDim: 256,
    maxDim: 2560,
    defaultWidth: 1024,
    defaultHeight: 1024,
    defaultPromptExtend: false,
  },
  "qwen-image-edit-spicy": {
    kind: "qwen_edit",
    vendor: "carrothub",
    maxReferenceImages: 2,
  },
  "face-swap": {
    kind: "face_swap",
    vendor: "carrothub",
  },
  "head-swap": {
    kind: "head_swap",
    vendor: "carrothub",
  },
};

const VIDEO_MODELS = Object.keys(VIDEO_MODEL_SPECS);
const IMAGE_MODELS = Object.keys(IMAGE_MODEL_SPECS);
const TASK_MODELS = VIDEO_MODELS.concat(IMAGE_MODELS);
const TEXT_MODEL = "carrothub-papaya-1.0";

const SECONDS_FIELD = {
  type: "number",
  unit: "second",
  description: { en: "Video generation unit price", zh: "视频生成单价" },
};
const VSR_SECONDS_FIELD = {
  type: "number",
  unit: "second",
  description: { en: "Video super-resolution unit price", zh: "视频超分单价" },
};
const IMAGE_COUNT_FIELD = {
  type: "number",
  unit: "count",
  unitLabel: { en: "image", zh: "张" },
  description: { en: "Image generation unit price", zh: "图片生成单价" },
};
const PROMPT_EXTEND_FIELD = {
  type: "boolean",
  description: { en: "Whether prompt optimization is enabled", zh: "是否开启提示词优化" },
};
const INPUT_IMAGES_FIELD = {
  enum: ["1", "2", "3"],
  enumLabels: {
    "1": { en: "1 input image", zh: "1 张输入图片" },
    "2": { en: "2 input images", zh: "2 张输入图片" },
    "3": { en: "3 input images", zh: "3 张输入图片" },
  },
  description: { en: "Input image count", zh: "输入图片数量" },
};
const RESOLUTION_DESCRIPTION = { en: "Output video resolution", zh: "输出视频分辨率" };

function resolutionField(resolutions) {
  const labels = {};
  for (const resolution of resolutions) labels[resolution] = { en: resolution, zh: resolution };
  return { enum: resolutions, enumLabels: labels, description: RESOLUTION_DESCRIPTION };
}

export const meta = {
  apiVersion: 1,
  key: "mulerouter",
  name: { en: "MuleRouter", zh: "MuleRouter" },
  description: {
    en: "MuleRouter video generation (Berry 1.0, Wan 2.7/2.2 Spicy, Wan 2.2 LoRA, Wan-Animate), FlashVSR super-resolution, image generation/editing/swap, and Papaya 1.0 chat completions",
    zh: "MuleRouter 视频生成（Berry 1.0、Wan 2.7/2.2 Spicy、Wan 2.2 LoRA、Wan-Animate）、FlashVSR 视频超分、图片生成/编辑/换脸换头与 Papaya 1.0 对话",
  },
  version: "1.0.0",
  author: { name: "AccelsRouter" },
  website: "https://mulerouter.ai/docs",
  baseUrl: "https://api.mulerouter.ai",
  upstreams: ["vendor", "new_api"],
  fetchMode: "per_task",
  keyHint: { en: "MuleRouter Inference API Key", zh: "MuleRouter 推理 API Key" },
  models: TASK_MODELS,
  relay: [
    { incoming_path: "/v1/chat/completions", upstream_path: "/vendors/openai/v1/chat/completions", models: [TEXT_MODEL] },
    {
      incoming_path: "/v1/responses",
      upstream_path: "/vendors/openai/v1/chat/completions",
      converter: "openai_responses_to_openai_chat_completions",
      models: [TEXT_MODEL],
    },
    {
      incoming_path: "/v1/messages",
      upstream_path: "/vendors/openai/v1/chat/completions",
      converter: "anthropic_messages_to_openai_chat_completions",
      models: [TEXT_MODEL],
    },
    {
      incoming_path: "/v1beta/models/{model}:generateContent",
      upstream_path: "/vendors/openai/v1/chat/completions",
      converter: "gemini_generate_content_to_openai_chat_completions",
      models: [TEXT_MODEL],
    },
  ],
  protocols: [
    { name: "openai_video", models: VIDEO_MODELS },
    { name: "openai_image", models: IMAGE_MODELS },
    { name: "openai_responses", supports: ["stream", "sync", "background"] },
  ],
  usageSchema: {
    seconds: SECONDS_FIELD,
    image_count: IMAGE_COUNT_FIELD,
  },
  usageProfiles: [
    {
      models: ["berry-1.0", "berry-1.0-turbo"],
      schema: { seconds: SECONDS_FIELD, resolution: resolutionField(["480p", "720p", "1080p"]) },
      examples: [
        { label: "480p · 5s", facts: { seconds: 5, resolution: "480p" } },
        { label: "720p · 5s", facts: { seconds: 5, resolution: "720p" } },
        { label: "1080p · 5s", facts: { seconds: 5, resolution: "1080p" } },
      ],
    },
    {
      models: ["berry-1.0-pro", "berry-1.0-pro-turbo"],
      schema: { seconds: SECONDS_FIELD, resolution: resolutionField(["1080p", "2k", "4k"]) },
      examples: [
        { label: "1080p · 5s", facts: { seconds: 5, resolution: "1080p" } },
        { label: "2k · 5s", facts: { seconds: 5, resolution: "2k" } },
        { label: "4k · 5s", facts: { seconds: 5, resolution: "4k" } },
      ],
    },
    {
      models: ["wan2.7-i2v-spicy"],
      schema: { seconds: SECONDS_FIELD, resolution: resolutionField(["720p", "1080p"]) },
      examples: [
        { label: "720p · 5s", facts: { seconds: 5, resolution: "720p" } },
        { label: "1080p · 5s", facts: { seconds: 5, resolution: "1080p" } },
      ],
    },
    {
      models: ["wan2.2-i2v-spicy"],
      schema: { seconds: SECONDS_FIELD, resolution: resolutionField(["480p", "720p"]), prompt_extend: PROMPT_EXTEND_FIELD },
      examples: [
        { label: "480p · 5s (prompt_extend)", facts: { seconds: 5, resolution: "480p", prompt_extend: true } },
        { label: "720p · 8s", facts: { seconds: 8, resolution: "720p", prompt_extend: false } },
      ],
    },
    {
      models: ["w2.2-i2v-lora", "wan-animate"],
      schema: { seconds: SECONDS_FIELD, resolution: resolutionField(["480p", "720p"]) },
      examples: [
        { label: "480p · 5s", facts: { seconds: 5, resolution: "480p" } },
        { label: "720p · 5s", facts: { seconds: 5, resolution: "720p" } },
      ],
    },
    {
      models: ["flashvsr"],
      schema: { seconds: VSR_SECONDS_FIELD, resolution: resolutionField(["720p", "1080p", "2k", "4k"]) },
      examples: [
        { label: "720p · 10s", facts: { seconds: 10, resolution: "720p" } },
        { label: "1080p · 10s", facts: { seconds: 10, resolution: "1080p" } },
        { label: "2k · 10s", facts: { seconds: 10, resolution: "2k" } },
        { label: "4k · 10s", facts: { seconds: 10, resolution: "4k" } },
      ],
    },
    {
      models: ["z-image-spicy"],
      schema: { image_count: IMAGE_COUNT_FIELD, prompt_extend: PROMPT_EXTEND_FIELD },
      examples: [
        { label: "1 image (prompt_extend)", facts: { image_count: 1, prompt_extend: true } },
        { label: "1 image", facts: { image_count: 1, prompt_extend: false } },
      ],
    },
    {
      models: ["z-image-spicy-pro", "face-swap", "head-swap"],
      schema: { image_count: IMAGE_COUNT_FIELD },
      examples: [{ label: "1 image", facts: { image_count: 1 } }],
    },
    {
      models: ["qwen-image-edit-spicy"],
      schema: { image_count: IMAGE_COUNT_FIELD, input_images: INPUT_IMAGES_FIELD },
      examples: [
        { label: "1 image (1 input)", facts: { image_count: 1, input_images: "1" } },
        { label: "1 image (2 inputs)", facts: { image_count: 1, input_images: "2" } },
        { label: "1 image (3 inputs)", facts: { image_count: 1, input_images: "3" } },
      ],
    },
  ],
  routes: [
    { method: "POST", path: "/vendors/:vendor/v1/:model/generation", type: "submit", decode: "createTask", render: "taskCreated" },
    { method: "GET", path: "/vendors/:vendor/v1/:model/generation/:task_id", type: "query", render: "taskStatus" },
  ],
};

function trimmed(value) {
  return String(value || "").trim();
}

function plainObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

function hasOwn(obj, key) {
  return Object.prototype.hasOwnProperty.call(obj, key);
}

function isFileRef(value) {
  return Boolean(value && typeof value === "object" && !Array.isArray(value) && trimmed(value.__fileRef));
}

function isNonEmptyMedia(value) {
  return isFileRef(value) || (typeof value === "string" && trimmed(value) !== "");
}

function mediaValue(value, fieldName) {
  if (isFileRef(value)) return value;
  if (typeof value === "string" && trimmed(value) !== "") return trimmed(value);
  throw new Error(fieldName + " must be a non-empty string");
}

function isHTTPURL(value) {
  return typeof value === "string" && /^https?:\/\/\S+$/i.test(trimmed(value));
}

function httpURLValue(value, fieldName) {
  if (typeof value !== "string" || !isHTTPURL(value)) throw new Error(fieldName + " must be an HTTP/HTTPS URL");
  return trimmed(value);
}

function specFor(model) {
  return VIDEO_MODEL_SPECS[model] || IMAGE_MODEL_SPECS[model] || null;
}

function upstreamModel(ctx) {
  return trimmed(ctx && ctx.upstreamModel) || trimmed(ctx && ctx.model) || trimmed(plainObject(ctx && ctx.requestBody).model);
}

function apiRoot(ctx) {
  const base = trimmed((ctx && ctx.baseUrl) || meta.baseUrl).replace(/\/+$/, "");
  if (ctx && ctx.upstream && ctx.upstream.kind === "new_api") return base + "/mulerouter";
  return base;
}

function submitHeaders(ctx) {
  return {
    Authorization: "Bearer " + trimmed(ctx && ctx.apiKey),
    "Content-Type": "application/json",
    Accept: "application/json",
  };
}

function queryHeaders(ctx) {
  return {
    Authorization: "Bearer " + trimmed(ctx && ctx.apiKey),
    Accept: "application/json",
  };
}

function unwrapRequest(ctx) {
  const raw = plainObject(ctx && ctx.requestBody);
  const metadata = plainObject(raw.metadata);
  return {
    raw: raw,
    merged: Object.assign({}, metadata, raw),
    metadata: metadata,
    native: plainObject(raw.native),
  };
}

function parseIntStrict(value, fieldName) {
  const n = typeof value === "string" && value.trim() !== "" ? Number(value) : value;
  if (typeof n !== "number" || !Number.isInteger(n)) throw new Error(fieldName + " must be an integer");
  return n;
}

function parseOptionalSeed(req) {
  if (req.seed === undefined || req.seed === null) return undefined;
  const seed = parseIntStrict(req.seed, "seed");
  if (seed < 0 || seed > MAX_SEED) throw new Error("seed must be an integer between 0 and " + MAX_SEED);
  return seed;
}

function parseOptionalBoolean(value, fieldName, fallback) {
  if (value === undefined || value === null) return fallback;
  if (typeof value !== "boolean") throw new Error(fieldName + " must be a boolean");
  return value;
}

function parseResolutionFromSize(size) {
  const raw = trimmed(size).toLowerCase();
  if (!raw) return "";
  if (["480p", "720p", "1080p", "2k", "4k"].includes(raw)) return raw;
  const parts = raw.replace("*", "x").split("x");
  if (parts.length !== 2) return "";
  const w = Number(parts[0]);
  const h = Number(parts[1]);
  if (!Number.isFinite(w) || !Number.isFinite(h) || w <= 0 || h <= 0) return "";
  const longSide = Math.max(w, h);
  if (longSide >= 3840) return "4k";
  if (longSide >= 2048) return "2k";
  if (longSide >= 1600) return "1080p";
  if (longSide >= 1000) return "720p";
  return "480p";
}

function parseRatioFromSize(size) {
  const raw = trimmed(size).toLowerCase();
  const parts = raw.replace("*", "x").split("x");
  if (parts.length !== 2) return "";
  const w = Number(parts[0]);
  const h = Number(parts[1]);
  if (!Number.isFinite(w) || !Number.isFinite(h) || w <= 0 || h <= 0) return "";
  const ratio = w / h;
  const candidates = [
    ["16:9", 16 / 9],
    ["4:3", 4 / 3],
    ["1:1", 1],
    ["3:4", 3 / 4],
    ["9:16", 9 / 16],
  ];
  let best = "";
  let bestDiff = Infinity;
  for (const [label, target] of candidates) {
    const diff = Math.abs(ratio - target);
    if (diff < bestDiff) {
      bestDiff = diff;
      best = label;
    }
  }
  return bestDiff <= 0.15 ? best : "";
}

function normalizeResolution(rawValue, allowed, fallback) {
  const lower = trimmed(rawValue).toLowerCase();
  const mapped = parseResolutionFromSize(lower) || lower;
  if (!mapped) {
    if (fallback) return fallback;
    throw new Error("resolution is required (" + allowed.join(", ") + ")");
  }
  if (!allowed.includes(mapped)) throw new Error("resolution must be one of: " + allowed.join(", "));
  return mapped;
}

function parseLoraList(value, fieldName) {
  if (value === undefined || value === null) return undefined;
  if (!Array.isArray(value)) throw new Error(fieldName + " must be an array");
  if (value.length > MAX_LORA_ITEMS) throw new Error(fieldName + " accepts at most " + MAX_LORA_ITEMS + " items");
  const out = [];
  for (let i = 0; i < value.length; i++) {
    const item = value[i];
    if (!item || typeof item !== "object" || Array.isArray(item)) throw new Error(fieldName + "[" + i + "] must be an object");
    const url = trimmed(item.url);
    if (!url) throw new Error(fieldName + "[" + i + "].url is required");
    const scale = item.scale === undefined || item.scale === null ? 1.0 : Number(item.scale);
    if (!Number.isFinite(scale) || scale < 0 || scale > 4) throw new Error(fieldName + "[" + i + "].scale must be between 0 and 4");
    out.push({ url: url, scale: scale });
  }
  return out;
}

function firstImageCandidate(req) {
  if (isNonEmptyMedia(req.image)) return req.image;
  if (isNonEmptyMedia(req.input_reference)) return req.input_reference;
  if (Array.isArray(req.images) && req.images.length > 0 && isNonEmptyMedia(req.images[0])) return req.images[0];
  return undefined;
}

function collectExtraImages(req, primaryConsumedFromImagesArray) {
  const list = [];
  if (Array.isArray(req.reference_images)) {
    for (const item of req.reference_images) list.push(item);
  } else if (Array.isArray(req.images)) {
    const start = primaryConsumedFromImagesArray ? 1 : 0;
    for (let i = start; i < req.images.length; i++) list.push(req.images[i]);
  }
  return list;
}

function convertBerryRequest(model, spec, merged, raw) {
  const body = {};
  if (hasOwn(merged, "prompt") && merged.prompt !== null && merged.prompt !== undefined) {
    if (typeof merged.prompt !== "string") throw new Error("prompt must be a string");
    if (trimmed(merged.prompt)) body.prompt = merged.prompt;
  }

  // Keyframe inputs: accept native first_frame / last_frame or protocol images if first_frame/reference_images absent.
  let firstFrame = merged.first_frame;
  let lastFrame = merged.last_frame;
  const hasExplicitRef =
    merged.reference_images != null ||
    merged.reference_videos != null ||
    merged.reference_audios != null ||
    isNonEmptyMedia(merged.file) ||
    isNonEmptyMedia(merged.link) ||
    (Array.isArray(merged.videos) && merged.videos.length > 0) ||
    (Array.isArray(merged.audios) && merged.audios.length > 0);

  if (!isNonEmptyMedia(firstFrame) && !isNonEmptyMedia(lastFrame) && !hasExplicitRef) {
    // If an OpenAI video/responses request passed image(s), decide keyframe (<=2 without video/audio refs) vs reference_images.
    const protoImages = [];
    if (isNonEmptyMedia(merged.image)) protoImages.push(merged.image);
    if (isNonEmptyMedia(merged.input_reference) && merged.input_reference !== merged.image) protoImages.push(merged.input_reference);
    if (Array.isArray(merged.images)) {
      for (const img of merged.images) {
        if (isNonEmptyMedia(img) && !protoImages.includes(img)) protoImages.push(img);
      }
    }
    if (protoImages.length === 1 || protoImages.length === 2) {
      firstFrame = protoImages[0];
      if (protoImages.length === 2) lastFrame = protoImages[1];
    } else if (protoImages.length > 2) {
      merged = Object.assign({}, merged, { reference_images: protoImages });
    }
  }

  const refImages = [];
  if (merged.reference_images !== undefined && merged.reference_images !== null) {
    if (!Array.isArray(merged.reference_images)) throw new Error("reference_images must be an array");
    if (merged.reference_images.length > 10) throw new Error("reference_images accepts at most 10 items");
    for (let i = 0; i < merged.reference_images.length; i++) refImages.push(mediaValue(merged.reference_images[i], "reference_images[" + i + "]"));
  } else if (hasExplicitRef) {
    if (isNonEmptyMedia(merged.image)) refImages.push(mediaValue(merged.image, "image"));
    if (isNonEmptyMedia(merged.input_reference) && merged.input_reference !== merged.image) refImages.push(mediaValue(merged.input_reference, "input_reference"));
    if (Array.isArray(merged.images)) {
      for (let i = 0; i < merged.images.length; i++) {
        const val = mediaValue(merged.images[i], "images[" + i + "]");
        if (!refImages.includes(val)) refImages.push(val);
      }
    }
    if (refImages.length > 10) throw new Error("reference_images accepts at most 10 items");
  }

  const rawVideos = merged.reference_videos !== undefined ? merged.reference_videos : merged.videos;
  const refVideos = [];
  if (rawVideos !== undefined && rawVideos !== null) {
    if (!Array.isArray(rawVideos)) throw new Error("reference_videos must be an array");
    if (rawVideos.length > 5) throw new Error("reference_videos accepts at most 5 items");
    for (let i = 0; i < rawVideos.length; i++) refVideos.push(httpURLValue(rawVideos[i], "reference_videos[" + i + "]"));
  }

  const rawAudios = merged.reference_audios !== undefined ? merged.reference_audios : merged.audios;
  const refAudios = [];
  if (rawAudios !== undefined && rawAudios !== null) {
    if (!Array.isArray(rawAudios)) throw new Error("reference_audios must be an array");
    if (rawAudios.length > 5) throw new Error("reference_audios accepts at most 5 items");
    for (let i = 0; i < rawAudios.length; i++) refAudios.push(httpURLValue(rawAudios[i], "reference_audios[" + i + "]"));
  }

  const fileVal = isNonEmptyMedia(merged.file) ? httpURLValue(merged.file, "file") : "";
  const linkVal = isNonEmptyMedia(merged.link) ? httpURLValue(merged.link, "link") : "";

  const hasKeyframe = isNonEmptyMedia(firstFrame) || isNonEmptyMedia(lastFrame);
  const hasReference = refImages.length > 0 || refVideos.length > 0 || refAudios.length > 0 || Boolean(fileVal) || Boolean(linkVal);

  if (hasKeyframe && hasReference) throw new Error("cannot mix keyframe fields (first_frame, last_frame) with reference fields");
  if (isNonEmptyMedia(lastFrame) && !isNonEmptyMedia(firstFrame)) throw new Error("last_frame requires first_frame");
  if (fileVal && linkVal) throw new Error("file and link are mutually exclusive");

  if (isNonEmptyMedia(firstFrame)) body.first_frame = mediaValue(firstFrame, "first_frame");
  if (isNonEmptyMedia(lastFrame)) body.last_frame = mediaValue(lastFrame, "last_frame");
  if (refImages.length) body.reference_images = refImages;
  if (refVideos.length) body.reference_videos = refVideos;
  if (refAudios.length) body.reference_audios = refAudios;
  if (fileVal) body.file = fileVal;
  if (linkVal) body.link = linkVal;

  if (!trimmed(body.prompt) && !hasKeyframe && !hasReference) {
    throw new Error(model + " requires at least one of prompt, first_frame, or reference inputs");
  }

  const resInput = merged.resolution !== undefined ? merged.resolution : raw.size || merged.size;
  body.resolution = normalizeResolution(resInput, spec.resolutions, spec.defaultResolution);

  let ratio = trimmed(merged.ratio).toLowerCase();
  if (!ratio && (raw.size || merged.size)) ratio = parseRatioFromSize(raw.size || merged.size);
  if (!ratio) ratio = "adaptive";
  if (!BERRY_RATIOS.includes(ratio)) throw new Error("ratio must be one of: " + BERRY_RATIOS.join(", "));
  body.ratio = ratio;

  const rawDuration = merged.duration !== undefined && merged.duration !== null ? merged.duration : merged.seconds;
  let duration = spec.defaultDuration;
  if (rawDuration !== undefined && rawDuration !== null) {
    duration = parseIntStrict(rawDuration, "duration");
    if (duration !== -1 && (duration < spec.minDuration || duration > spec.maxDuration)) {
      throw new Error("duration must be -1 or an integer from " + spec.minDuration + " to " + spec.maxDuration);
    }
  }
  body.duration = duration;
  body.audio = parseOptionalBoolean(merged.audio, "audio", true);
  body.prompt_extend = parseOptionalBoolean(merged.prompt_extend, "prompt_extend", true);
  const seed = parseOptionalSeed(merged);
  if (seed !== undefined) body.seed = seed;

  const billedDuration = (duration === -1 ? spec.maxDuration : duration) + (refVideos.length > 0 ? spec.referenceVideoMaxReserve : 0);
  const action = hasReference ? "reference_to_video" : hasKeyframe ? "image_to_video" : "text_to_video";
  return {
    body: body,
    action: action,
    facts: { seconds: billedDuration, resolution: body.resolution },
  };
}

function convertWan27Request(model, spec, merged, raw) {
  const prompt = typeof merged.prompt === "string" ? merged.prompt : "";
  if (!trimmed(prompt)) throw new Error("prompt is required");
  const rawImage = firstImageCandidate(merged);
  if (!isNonEmptyMedia(rawImage)) throw new Error(model + " requires image");

  const body = {
    prompt: prompt,
    image: mediaValue(rawImage, "image"),
    resolution: normalizeResolution(merged.resolution !== undefined ? merged.resolution : raw.size || merged.size, spec.resolutions, spec.defaultResolution),
  };

  const rawDuration = merged.duration !== undefined && merged.duration !== null ? merged.duration : merged.seconds;
  let duration = spec.defaultDuration;
  if (rawDuration !== undefined && rawDuration !== null) {
    duration = parseIntStrict(rawDuration, "duration");
    if (duration < spec.minDuration || duration > spec.maxDuration) {
      throw new Error("duration must be an integer from " + spec.minDuration + " to " + spec.maxDuration);
    }
  }
  body.duration = duration;
  body.audio = parseOptionalBoolean(merged.audio, "audio", true);
  body.prompt_extend = parseOptionalBoolean(merged.prompt_extend, "prompt_extend", true);

  if (merged.negative_prompt !== undefined && merged.negative_prompt !== null) {
    if (typeof merged.negative_prompt !== "string") throw new Error("negative_prompt must be a string");
    if (trimmed(merged.negative_prompt)) body.negative_prompt = merged.negative_prompt;
  }
  if (merged.audio_url !== undefined && merged.audio_url !== null && trimmed(merged.audio_url)) {
    body.audio_url = httpURLValue(merged.audio_url, "audio_url");
  }
  const seed = parseOptionalSeed(merged);
  if (seed !== undefined) body.seed = seed;

  return {
    body: body,
    action: "image_to_video",
    facts: { seconds: duration, resolution: body.resolution },
  };
}

function convertWan22Request(model, spec, merged, raw) {
  const prompt = typeof merged.prompt === "string" ? merged.prompt : "";
  if (!trimmed(prompt)) throw new Error("prompt is required");
  const rawImage = firstImageCandidate(merged);
  if (!isNonEmptyMedia(rawImage)) throw new Error(model + " requires image");

  let rawLast = merged.last_image;
  if (!isNonEmptyMedia(rawLast) && Array.isArray(merged.images) && merged.images.length > 1 && isNonEmptyMedia(merged.images[1]) && merged.images[0] === rawImage) {
    rawLast = merged.images[1];
  }

  const body = {
    image: mediaValue(rawImage, "image"),
    prompt: prompt,
    resolution: normalizeResolution(merged.resolution !== undefined ? merged.resolution : raw.size || merged.size, spec.resolutions, spec.defaultResolution),
  };
  if (isNonEmptyMedia(rawLast)) body.last_image = mediaValue(rawLast, "last_image");

  const rawDuration = merged.duration !== undefined && merged.duration !== null ? merged.duration : merged.seconds;
  let duration = spec.defaultDuration;
  if (rawDuration !== undefined && rawDuration !== null) {
    duration = parseIntStrict(rawDuration, "duration");
    if (!spec.allowedDurations.includes(duration)) {
      throw new Error("duration must be one of: " + spec.allowedDurations.join(", "));
    }
  }
  body.duration = duration;
  body.prompt_extend = parseOptionalBoolean(merged.prompt_extend, "prompt_extend", true);
  const seed = parseOptionalSeed(merged);
  if (seed !== undefined) body.seed = seed;

  return {
    body: body,
    action: "image_to_video",
    facts: { seconds: duration, resolution: body.resolution, prompt_extend: body.prompt_extend },
  };
}

function convertLoraRequest(model, spec, merged, raw) {
  const prompt = typeof merged.prompt === "string" ? merged.prompt : "";
  if (!trimmed(prompt)) throw new Error("prompt is required");
  const rawImage = firstImageCandidate(merged);
  if (!isNonEmptyMedia(rawImage)) throw new Error(model + " requires image");

  const body = {
    prompt: prompt,
    image: mediaValue(rawImage, "image"),
    resolution: normalizeResolution(merged.resolution !== undefined ? merged.resolution : raw.size || merged.size, spec.resolutions, spec.defaultResolution),
  };

  const rawDuration = merged.duration !== undefined && merged.duration !== null ? merged.duration : merged.seconds;
  let duration = spec.defaultDuration;
  if (rawDuration !== undefined && rawDuration !== null) {
    duration = parseIntStrict(rawDuration, "duration");
    if (!spec.allowedDurations.includes(duration)) {
      throw new Error("duration must be one of: " + spec.allowedDurations.join(", "));
    }
  }
  body.duration = duration;

  for (const field of ["loras", "high_noise_loras", "low_noise_loras"]) {
    const parsed = parseLoraList(merged[field], field);
    if (parsed !== undefined) body[field] = parsed;
  }
  const seed = parseOptionalSeed(merged);
  if (seed !== undefined) body.seed = seed;

  return {
    body: body,
    action: "image_to_video",
    facts: { seconds: duration, resolution: body.resolution },
  };
}

function convertAnimateRequest(model, spec, merged, raw) {
  const rawImage = firstImageCandidate(merged);
  if (!isNonEmptyMedia(rawImage)) throw new Error(model + " requires image");
  let rawVideo = merged.video;
  if (!isNonEmptyMedia(rawVideo) && Array.isArray(merged.videos) && merged.videos.length > 0) rawVideo = merged.videos[0];
  if (!trimmed(rawVideo)) throw new Error(model + " requires video");

  const mode = trimmed(merged.mode).toLowerCase() || "animate";
  if (mode !== "animate" && mode !== "replace") throw new Error("mode must be animate or replace");

  const body = {
    image: mediaValue(rawImage, "image"),
    video: httpURLValue(rawVideo, "video"),
    mode: mode,
    resolution: normalizeResolution(merged.resolution !== undefined ? merged.resolution : raw.size || merged.size, spec.resolutions, spec.defaultResolution),
  };
  if (merged.prompt !== undefined && merged.prompt !== null) {
    if (typeof merged.prompt !== "string") throw new Error("prompt must be a string");
    if (trimmed(merged.prompt)) body.prompt = merged.prompt;
  }
  const seed = parseOptionalSeed(merged);
  if (seed !== undefined) body.seed = seed;

  return {
    body: body,
    action: "video_to_video",
    facts: { seconds: spec.maxDuration, resolution: body.resolution },
  };
}

function convertFlashVSRRequest(model, spec, merged, raw) {
  let rawVideo = merged.video;
  if (!isNonEmptyMedia(rawVideo) && Array.isArray(merged.videos) && merged.videos.length > 0) rawVideo = merged.videos[0];
  if (!trimmed(rawVideo)) throw new Error(model + " requires video");

  const rawRes = merged.resolution !== undefined ? merged.resolution : merged.target_resolution !== undefined ? merged.target_resolution : raw.size || merged.size;
  const resolution = normalizeResolution(rawRes, spec.resolutions, "");

  return {
    body: {
      video: httpURLValue(rawVideo, "video"),
      resolution: resolution,
    },
    action: "video_to_video",
    facts: { seconds: spec.maxDuration, resolution: resolution },
  };
}

function parseDimension(value, fallback, minDim, maxDim, fieldName) {
  if (value === undefined || value === null || value === "") return fallback;
  const n = parseIntStrict(value, fieldName);
  if (n < minDim || n > maxDim) throw new Error(fieldName + " must be an integer between " + minDim + " and " + maxDim);
  return n;
}

function parseImageSize(size, spec) {
  const s = trimmed(size);
  if (!s) return { width: spec.defaultWidth, height: spec.defaultHeight };
  const parts = s.toLowerCase().replace("*", "x").split("x");
  if (parts.length !== 2) throw new Error("size must be WIDTHxHEIGHT");
  return {
    width: parseDimension(parts[0], spec.defaultWidth, spec.minDim, spec.maxDim, "width"),
    height: parseDimension(parts[1], spec.defaultHeight, spec.minDim, spec.maxDim, "height"),
  };
}

function convertZImageRequest(model, spec, merged, raw) {
  const prompt = typeof merged.prompt === "string" ? merged.prompt : "";
  if (!trimmed(prompt)) throw new Error("prompt is required");

  const dims = raw.size || merged.size ? parseImageSize(raw.size || merged.size, spec) : { width: spec.defaultWidth, height: spec.defaultHeight };
  const width = merged.width !== undefined && merged.width !== null ? parseDimension(merged.width, spec.defaultWidth, spec.minDim, spec.maxDim, "width") : dims.width;
  const height = merged.height !== undefined && merged.height !== null ? parseDimension(merged.height, spec.defaultHeight, spec.minDim, spec.maxDim, "height") : dims.height;

  const body = {
    prompt: prompt,
    width: width,
    height: height,
  };
  const promptExtend = parseOptionalBoolean(merged.prompt_extend, "prompt_extend", spec.defaultPromptExtend);
  body.prompt_extend = promptExtend;
  const seed = parseOptionalSeed(merged);
  if (seed !== undefined) body.seed = seed;

  const facts = { image_count: 1 };
  if (spec.kind === "zimage") facts.prompt_extend = promptExtend;
  return {
    body: body,
    action: "text_to_image",
    facts: facts,
  };
}

function convertQwenEditRequest(model, spec, merged) {
  const prompt = typeof merged.prompt === "string" ? merged.prompt : "";
  if (!trimmed(prompt)) throw new Error("prompt is required");
  const rawImage = firstImageCandidate(merged);
  if (!isNonEmptyMedia(rawImage)) throw new Error(model + " requires image");

  const primaryFromArray = !isNonEmptyMedia(merged.image) && !isNonEmptyMedia(merged.input_reference) && Array.isArray(merged.images) && merged.images.length > 0;
  const extras = collectExtraImages(merged, primaryFromArray);
  if (extras.length > spec.maxReferenceImages) {
    throw new Error("reference_images accepts at most " + spec.maxReferenceImages + " items");
  }

  const body = {
    image: mediaValue(rawImage, "image"),
    prompt: prompt,
  };
  if (extras.length > 0) {
    body.reference_images = extras.map(function (item, idx) {
      return mediaValue(item, "reference_images[" + idx + "]");
    });
  }
  const seed = parseOptionalSeed(merged);
  if (seed !== undefined) body.seed = seed;

  return {
    body: body,
    action: "image_to_image",
    facts: { image_count: 1, input_images: String(1 + extras.length) },
  };
}

function convertSwapRequest(model, spec, merged) {
  const rawImage = firstImageCandidate(merged);
  if (!isNonEmptyMedia(rawImage)) throw new Error(model + " requires image");
  const secondField = spec.kind === "face_swap" ? "face_image" : "head_image";
  let rawSecond = merged[secondField];
  if (!isNonEmptyMedia(rawSecond) && Array.isArray(merged.images) && merged.images.length > 1 && merged.images[0] === rawImage) {
    rawSecond = merged.images[1];
  } else if (!isNonEmptyMedia(rawSecond) && Array.isArray(merged.reference_images) && merged.reference_images.length > 0) {
    rawSecond = merged.reference_images[0];
  }
  if (!isNonEmptyMedia(rawSecond)) throw new Error(model + " requires " + secondField);

  const body = {
    image: mediaValue(rawImage, "image"),
  };
  body[secondField] = mediaValue(rawSecond, secondField);
  const seed = parseOptionalSeed(merged);
  if (seed !== undefined) body.seed = seed;

  return {
    body: body,
    action: "image_to_image",
    facts: { image_count: 1 },
  };
}

function convertRequest(ctx) {
  const model = upstreamModel(ctx);
  if (!model) throw new Error("model is required");
  const spec = specFor(model);
  if (!spec) throw new Error("unsupported model: " + model);

  const { raw, merged, native } = unwrapRequest(ctx);
  if (native.vendor && !trimmed(ctx && ctx.upstreamModel) && trimmed(native.vendor) !== spec.vendor) {
    throw new Error("model " + model + " belongs to vendor " + spec.vendor + ", not " + native.vendor);
  }

  let result;
  switch (spec.kind) {
    case "berry":
      result = convertBerryRequest(model, spec, merged, raw);
      break;
    case "wan27":
      result = convertWan27Request(model, spec, merged, raw);
      break;
    case "wan22":
      result = convertWan22Request(model, spec, merged, raw);
      break;
    case "lora":
      result = convertLoraRequest(model, spec, merged, raw);
      break;
    case "animate":
      result = convertAnimateRequest(model, spec, merged, raw);
      break;
    case "flashvsr":
      result = convertFlashVSRRequest(model, spec, merged, raw);
      break;
    case "zimage":
    case "zimage_pro":
      result = convertZImageRequest(model, spec, merged, raw);
      break;
    case "qwen_edit":
      result = convertQwenEditRequest(model, spec, merged);
      break;
    case "face_swap":
    case "head_swap":
      result = convertSwapRequest(model, spec, merged);
      break;
    default:
      throw new Error("unsupported model kind: " + spec.kind);
  }

  return Object.assign({ model: model, vendor: spec.vendor }, result);
}

function formatError(errObj, fallback) {
  const err = plainObject(errObj);
  if (typeof errObj === "string" && trimmed(errObj)) return trimmed(errObj);
  const parts = [];
  const title = trimmed(err.title);
  const detail = trimmed(err.detail || err.message);
  if (title && detail && title !== detail) parts.push(title + ": " + detail);
  else if (detail || title) parts.push(detail || title);
  const code = err.code !== undefined ? err.code : err.error_code;
  if (code !== undefined && code !== null && trimmed(code) !== "" && parts.length) {
    return "[" + code + "] " + parts[0];
  }
  return parts[0] || fallback;
}

function extractVideos(body) {
  const obj = plainObject(body);
  const urls = [];
  if (Array.isArray(obj.videos)) {
    for (const item of obj.videos) {
      if (typeof item === "string" && trimmed(item)) urls.push(trimmed(item));
      else if (item && typeof item === "object" && trimmed(item.url)) urls.push(trimmed(item.url));
    }
  }
  const output = plainObject(obj.output);
  if (!urls.length && trimmed(output.video_url)) urls.push(trimmed(output.video_url));
  if (!urls.length && trimmed(output.url)) urls.push(trimmed(output.url));
  return urls;
}

function extractImages(body) {
  const obj = plainObject(body);
  const urls = [];
  if (Array.isArray(obj.images)) {
    for (const item of obj.images) {
      if (typeof item === "string" && trimmed(item)) urls.push(trimmed(item));
      else if (item && typeof item === "object" && trimmed(item.url)) urls.push(trimmed(item.url));
    }
  }
  if (!urls.length && Array.isArray(obj.results)) {
    for (const item of obj.results) {
      if (typeof item === "string" && trimmed(item)) urls.push(trimmed(item));
      else if (item && typeof item === "object" && trimmed(item.url)) urls.push(trimmed(item.url));
    }
  }
  const output = plainObject(obj.output);
  if (!urls.length && trimmed(output.url)) urls.push(trimmed(output.url));
  return urls;
}

function modelIsVideo(model) {
  return Boolean(VIDEO_MODEL_SPECS[model]);
}

function modelIsImage(model) {
  return Boolean(IMAGE_MODEL_SPECS[model]);
}

function reportedNumber(value) {
  const n = typeof value === "string" && trimmed(value) !== "" ? Number(value) : value;
  return typeof n === "number" && Number.isFinite(n) && n > 0 ? n : 0;
}

export function buildSubmitRequest(ctx) {
  const converted = convertRequest(ctx);
  return {
    url: apiRoot(ctx) + "/vendors/" + converted.vendor + "/v1/" + converted.model + "/generation",
    method: "POST",
    headers: submitHeaders(ctx),
    body: converted.body,
    action: converted.action,
  };
}

export function parseSubmitResponse(ctx, resp) {
  const body = plainObject(resp && resp.body);
  const info = plainObject(body.task_info);
  if (info.status === "failed" || info.error || body.error) {
    throw new Error(formatError(info.error || body.error, "submission rejected"));
  }
  if (resp && resp.status >= 400) {
    throw new Error(formatError(body.detail || body, "upstream HTTP " + resp.status));
  }
  const taskId = trimmed(info.id || body.task_id || body.id);
  if (!taskId) throw new Error("task_info.id is missing in submit response");
  let state = null;
  if (ctx && ctx.requestBody) {
    const converted = convertRequest(ctx);
    state = {
      facts: converted.facts,
      hasReferenceVideos: Boolean(converted.body.reference_videos && converted.body.reference_videos.length > 0),
    };
  }
  return { taskId: taskId, taskData: body, state: state };
}

export function extractUsage(ctx) {
  return convertRequest(ctx).facts;
}

export function extractUsageOnComplete(task, _taskResult, body) {
  const model = upstreamModel(task);
  if (modelIsImage(model)) return { image_count: 1 };
  const spec = specFor(model);
  if (!spec || !modelIsVideo(model)) return {};

  const rawState = plainObject(task && task.state);
  const stateFacts = plainObject(rawState.facts || rawState);
  const hasReferenceVideos = rawState.hasReferenceVideos === true;
  const fallbackCap = (spec.maxDuration || 30) + (spec.referenceVideoMaxReserve || 0);
  const reservedSeconds = reportedNumber(stateFacts.seconds) || fallbackCap;
  const obj = plainObject(body);
  const usage = plainObject(obj.usage);
  const info = plainObject(obj.task_info);

  let reportedSeconds = 0;
  const total = reportedNumber(usage.total_duration !== undefined ? usage.total_duration : usage.total_seconds);
  if (total > 0) {
    reportedSeconds = total;
  } else {
    const inputDur = reportedNumber(usage.input_video_duration !== undefined ? usage.input_video_duration : usage.input_seconds);
    const outputDur = reportedNumber(
      usage.output_video_duration !== undefined
        ? usage.output_video_duration
        : usage.output_seconds !== undefined
          ? usage.output_seconds
          : usage.duration !== undefined
            ? usage.duration
            : usage.seconds
    );
    if (inputDur > 0 && outputDur > 0) {
      reportedSeconds = inputDur + outputDur;
    } else if (!hasReferenceVideos) {
      const single = reportedNumber(outputDur > 0 ? outputDur : info.duration !== undefined ? info.duration : obj.duration);
      if (single > 0) reportedSeconds = single;
    }
  }

  if (reportedSeconds <= 0) return {};
  return Object.assign({}, stateFacts, { seconds: Math.min(reportedSeconds, reservedSeconds) });
}

export function buildQueryRequest(ctx) {
  const model = upstreamModel(ctx);
  const spec = specFor(model);
  if (!spec) throw new Error("unsupported model: " + model);
  return {
    url: apiRoot(ctx) + "/vendors/" + spec.vendor + "/v1/" + model + "/generation/" + encodeURIComponent(trimmed(ctx.taskId)),
    method: "GET",
    headers: queryHeaders(ctx),
  };
}

export function parseTaskResult(ctx, body) {
  const obj = plainObject(body);
  const info = plainObject(obj.task_info);
  const status = trimmed(info.status || obj.status).toLowerCase();

  if (status === "pending" || status === "queued") return { status: "SUBMITTED" };
  if (status === "processing" || status === "running") return { status: "IN_PROGRESS" };
  if (status === "completed" || status === "succeeded") {
    const model = upstreamModel(ctx);
    const urls = modelIsImage(model) ? extractImages(obj) : extractVideos(obj);
    if (!urls.length) {
      const fallback = modelIsImage(model) ? extractVideos(obj) : extractImages(obj);
      if (!fallback.length) return { status: "FAILURE", reason: "task completed without media output" };
      return { status: "SUCCESS", url: fallback[0], progress: "100%" };
    }
    return { status: "SUCCESS", url: urls[0], progress: "100%" };
  }
  if (status === "failed" || status === "cancelled" || status === "canceled") {
    return { status: "FAILURE", reason: formatError(info.error || obj.error, "task failed") };
  }
  if (obj.error) {
    return { status: "FAILURE", reason: formatError(obj.error, "task failed") };
  }
  return { status: "UNKNOWN", reason: "unrecognized status: " + status };
}

export function listArtifacts(task) {
  if (!task || task.status !== "SUCCESS") return [];
  const data = plainObject(task.data);
  const model = upstreamModel(task);
  if (modelIsImage(model) || (!modelIsVideo(model) && Array.isArray(data.images) && data.images.length > 0)) {
    const images = extractImages(data);
    return images.map(function (_url, index) {
      return { key: "image-" + (index + 1), type: "image" };
    });
  }
  const videos = extractVideos(data);
  return videos.length ? [{ key: "video", type: "video" }] : [];
}

export function buildContentRequest(ctx) {
  const data = plainObject(ctx && ctx.data);
  let url = "";
  if (ctx.artifactKey === "video") {
    const videos = extractVideos(data);
    url = videos[0] || "";
  } else if (String(ctx.artifactKey || "").startsWith("image-")) {
    const images = extractImages(data);
    const idx = Number(String(ctx.artifactKey).slice("image-".length)) - 1;
    if (Number.isInteger(idx) && idx >= 0 && idx < images.length) url = images[idx];
  }
  if (!url) throw new Error("artifact_not_found");
  return { url: url, method: (ctx.clientRequest && ctx.clientRequest.method) || "GET", credentialless: true };
}

function nativeRenderedStatus(task) {
  const data = plainObject(task && task.data);
  const info = Object.assign({}, plainObject(data.task_info), { id: task.task_id });
  const mappedStatus = {
    NOT_START: "pending",
    SUBMITTED: "pending",
    QUEUED: "pending",
    IN_PROGRESS: "processing",
    SUCCESS: "completed",
    FAILURE: "failed",
  }[task.status];
  if (mappedStatus && (!info.status || task.status === "FAILURE")) {
    info.status = mappedStatus;
  }
  if (task.status === "FAILURE" && !info.error && task.fail_reason) {
    info.error = { code: 500, title: "Task Failed", detail: task.fail_reason };
  }
  const out = Object.assign({}, data, { task_info: info });
  if (task.status === "SUCCESS" && Array.isArray(data.videos)) {
    const artifact = task.artifacts && task.artifacts.video;
    if (artifact && trimmed(artifact.url)) out.videos = [trimmed(artifact.url)];
  }
  if (task.status === "SUCCESS" && Array.isArray(data.images)) {
    const rewritten = [];
    for (let i = 0; i < data.images.length; i++) {
      const artifact = task.artifacts && task.artifacts["image-" + (i + 1)];
      rewritten.push(artifact && trimmed(artifact.url) ? trimmed(artifact.url) : data.images[i]);
    }
    out.images = rewritten;
  }
  return out;
}

export const native = {
  createTask: function (ctx) {
    if (!ctx.body || ctx.body.kind !== "json" || !ctx.body.value || typeof ctx.body.value !== "object" || Array.isArray(ctx.body.value)) {
      throw new Error("JSON object required");
    }
    const req = ctx.body.value;
    const model = trimmed((ctx.params && ctx.params.model) || req.model);
    if (!model) throw new Error("model is required");
    const requestBody = Object.assign({}, req, {
      model: model,
      native: { vendor: trimmed(ctx.params && ctx.params.vendor) },
    });
    return {
      kind: "submit",
      model: model,
      action: VIDEO_MODEL_SPECS[model] ? "generate_video" : "generate_image",
      requestBody: requestBody,
    };
  },
  taskCreated: function (_ctx, task) {
    return nativeRenderedStatus(task);
  },
  taskStatus: function (_ctx, task) {
    return nativeRenderedStatus(task);
  },
};

function imageDatum(image) {
  if (/^https?:\/\//i.test(image)) return { url: image };
  const match = /^data:[^;,]*;base64,(.*)$/s.exec(image);
  return { b64_json: match ? match[1] : image };
}

function responsesInput(req) {
  const texts = [],
    images = [],
    videos = [],
    audios = [];
  const input = req.input;
  if (typeof input === "string") texts.push(input);
  else if (Array.isArray(input)) {
    for (const item of input) {
      if (typeof item === "string") {
        texts.push(item);
        continue;
      }
      if (!item || typeof item !== "object" || Array.isArray(item)) continue;
      const content = item.content === undefined ? [item] : Array.isArray(item.content) ? item.content : [item.content];
      for (const part of content) {
        if (typeof part === "string") {
          texts.push(part);
          continue;
        }
        if (!part || typeof part !== "object" || Array.isArray(part)) continue;
        if (["input_text", "text"].includes(part.type) && typeof part.text === "string") texts.push(part.text);
        if (["input_image", "image_url"].includes(part.type)) {
          let image = part.image_url;
          if (image && typeof image === "object") image = image.url;
          if (trimmed(image)) images.push(trimmed(image));
        }
        if (["input_video", "video_url"].includes(part.type)) {
          let video = part.video_url;
          if (video && typeof video === "object") video = video.url;
          if (trimmed(video)) videos.push(trimmed(video));
        }
        if (["input_audio", "audio_url"].includes(part.type)) {
          let audio = part.audio_url;
          if (audio && typeof audio === "object") audio = audio.url;
          if (trimmed(audio)) audios.push(trimmed(audio));
        }
      }
    }
  }
  return {
    prompt: texts
      .filter(function (text) {
        return trimmed(text);
      })
      .join("\n"),
    images: images,
    videos: videos,
    audios: audios,
  };
}

function responsesOutputText(ctx, task) {
  const model = upstreamModel(task) || upstreamModel(ctx);
  if (modelIsImage(model)) {
    const data = plainObject(task && task.data);
    const images = extractImages(data);
    if (!images.length) throw new Error("image artifact is unavailable");
    return images
      .map(function (_url, index) {
        const artifact = ctx && ctx.artifacts && ctx.artifacts["image-" + (index + 1)];
        const url = trimmed(artifact && artifact.url);
        if (!url) throw new Error("image artifact is unavailable");
        return "![Image " + (index + 1) + "](<" + url + ">)";
      })
      .join("\n\n");
  }
  const artifact = ctx && ctx.artifacts && ctx.artifacts.video;
  const url = trimmed(artifact && artifact.url);
  if (!url) throw new Error("video artifact is unavailable");
  const escaped = url
    .replace(/&/g, "&amp;")
    .replace(/\u0022/g, "&quot;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
  return '<video controls src="' + escaped + '"></video>';
}

function parseMultipartForm(ctx, filePolicy) {
  const fields = plainObject(ctx.body && ctx.body.fields);
  const req = {};
  for (const name of Object.keys(fields)) {
    const values = fields[name] || [];
    if (["images", "reference_images", "reference_videos", "reference_audios"].includes(name)) {
      req[name] = values;
    } else {
      if (values.length > 1) throw new Error(name + " must be provided once");
      req[name] = values[0];
    }
  }
  if (req.metadata !== undefined) {
    let parsed;
    try {
      parsed = JSON.parse(req.metadata);
    } catch (e) {
      throw new Error("metadata must be a JSON object string");
    }
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("metadata must be a JSON object string");
    req.metadata = parsed;
  }
  for (const key of ["seconds", "duration", "seed", "n", "width", "height"]) {
    if (req[key] !== undefined && req[key] !== "") req[key] = Number(req[key]);
  }
  for (const key of ["audio", "prompt_extend"]) {
    if (req[key] === undefined) continue;
    if (req[key] !== "true" && req[key] !== "false") throw new Error(key + " must be true or false");
    req[key] = req[key] === "true";
  }
  filePolicy(req, (ctx.body && ctx.body.files) || []);
  return req;
}

const FORWARDED_FIELDS = [
  "size",
  "resolution",
  "target_resolution",
  "ratio",
  "duration",
  "seconds",
  "width",
  "height",
  "audio",
  "audio_url",
  "prompt_extend",
  "negative_prompt",
  "seed",
  "mode",
  "video",
  "file",
  "link",
  "first_frame",
  "last_frame",
  "last_image",
  "face_image",
  "head_image",
  "reference_images",
  "reference_videos",
  "reference_audios",
  "loras",
  "high_noise_loras",
  "low_noise_loras",
  "metadata",
];

function uploadedImageRef(file) {
  const rawMime = trimmed(file.mimeType).toLowerCase();
  return {
    __fileRef: file.ref,
    encoding: "dataUrl",
    mimeType: /^image\/[a-z0-9.+-]+$/.test(rawMime) ? rawMime : "image/png",
    maxBytes: MAX_INPUT_IMAGE_BYTES,
  };
}

export const protocols = {
  openai_video: {
    decodeRequest: function (ctx) {
      let req;
      const uploadedImages = [];
      if (ctx.body && ctx.body.kind === "json") {
        req = ctx.body.value;
        if (!req || typeof req !== "object" || Array.isArray(req)) throw new Error("request body must be an object");
      } else if (ctx.body && ctx.body.kind === "multipart") {
        req = parseMultipartForm(ctx, function (target, files) {
          for (const file of files) {
            if (file.field === "video") throw new Error("video uploads must be passed as an HTTP/HTTPS URL");
            const upload = uploadedImageRef(file);
            if (file.field === "first_frame" && !target.first_frame) target.first_frame = upload;
            else if (file.field === "last_frame" && !target.last_frame) target.last_frame = upload;
            else if (file.field === "last_image" && !target.last_image) target.last_image = upload;
            else uploadedImages.push(upload);
          }
        });
      } else {
        throw new Error("JSON or multipart body required");
      }

      const requestBody = { model: ctx.model };
      if (req.prompt !== undefined) requestBody.prompt = req.prompt;
      const images = [];
      for (const img of [].concat(
        isNonEmptyMedia(req.image) ? [req.image] : [],
        isNonEmptyMedia(req.input_reference) ? [req.input_reference] : [],
        Array.isArray(req.images) ? req.images : [],
        uploadedImages
      )) {
        if (isNonEmptyMedia(img)) images.push(img);
      }
      if (images.length === 1 && !req.image) requestBody.image = images[0];
      else if (images.length > 0) requestBody.images = images;

      for (const field of FORWARDED_FIELDS) {
        if (hasOwn(req, field)) requestBody[field] = req[field];
      }
      const hasMedia = images.length > 0 || isNonEmptyMedia(requestBody.first_frame) || isNonEmptyMedia(requestBody.video);
      return {
        kind: "submit",
        model: ctx.model,
        action: hasMedia ? "image_to_video" : "text_to_video",
        requestBody: requestBody,
      };
    },
    render: function (_ctx, task) {
      const statuses = {
        NOT_START: "queued",
        SUBMITTED: "queued",
        QUEUED: "queued",
        IN_PROGRESS: "in_progress",
        SUCCESS: "completed",
        FAILURE: "failed",
      };
      const output = {
        id: task.task_id,
        object: "video",
        model: (task.properties && task.properties.origin_model_name) || "",
        status: statuses[task.status] || "unknown",
        progress: Number(String(task.progress || "0").replace("%", "")) || 0,
        created_at: Number(task.created_at || 0),
      };
      const completedAt = Number(task.finished_at || task.updated_at || 0);
      if (completedAt > 0) output.completed_at = completedAt;
      if (task.status === "FAILURE") {
        const infoErr = plainObject(plainObject(task.data && task.data.task_info).error);
        const topErr = plainObject(task.data && task.data.error);
        const msg = trimmed(task.fail_reason) || formatError(infoErr.detail ? infoErr : topErr, "video generation failed");
        output.error = { code: String(infoErr.code || topErr.code || "video_generation_failed"), message: msg };
      }
      return output;
    },
  },
  openai_image: {
    decodeRequest: function (ctx) {
      const model = trimmed(ctx.model);
      if (!model) throw new Error("model is required");
      let req = {};
      const uploads = [];
      if (ctx.body && ctx.body.kind === "json") {
        req = ctx.body.value;
        if (!req || typeof req !== "object" || Array.isArray(req)) throw new Error("request body must be an object");
      } else if (ctx.body && ctx.body.kind === "multipart") {
        req = parseMultipartForm(ctx, function (target, files) {
          for (const file of files) {
            const upload = uploadedImageRef(file);
            if (file.field === "face_image" && !target.face_image) target.face_image = upload;
            else if (file.field === "head_image" && !target.head_image) target.head_image = upload;
            else if (file.field === "image" && !target.image) target.image = upload;
            else uploads.push(upload);
          }
        });
      } else {
        throw new Error("JSON or multipart body required");
      }

      if (req.stream !== undefined && req.stream !== false && req.stream !== "false") {
        throw new Error("stream is not supported on images endpoints");
      }
      if (req.response_format !== undefined && req.response_format !== "url" && req.response_format !== "b64_json") {
        throw new Error("response_format must be url or b64_json");
      }
      const n = req.n === undefined || req.n === null ? 1 : req.n;
      if (!Number.isInteger(n) || n !== 1) throw new Error("n must be 1");

      const requestBody = { model: model };
      if (req.prompt !== undefined) requestBody.prompt = req.prompt;
      if (isNonEmptyMedia(req.image)) requestBody.image = req.image;
      const extraRefs = [];
      if (Array.isArray(req.reference_images)) {
        for (const item of req.reference_images) if (isNonEmptyMedia(item)) extraRefs.push(item);
      }
      if (Array.isArray(req.images)) {
        for (const item of req.images) {
          if (!isNonEmptyMedia(item)) continue;
          if (!requestBody.image) requestBody.image = item;
          else extraRefs.push(item);
        }
      }
      for (const upload of uploads) {
        if (!requestBody.image) requestBody.image = upload;
        else extraRefs.push(upload);
      }
      if (extraRefs.length) requestBody.reference_images = extraRefs;

      for (const field of ["size", "width", "height", "seed", "prompt_extend", "face_image", "head_image", "metadata"]) {
        if (hasOwn(req, field)) requestBody[field] = req[field];
      }

      const isEdit = Boolean(requestBody.image || requestBody.face_image || requestBody.head_image || extraRefs.length);
      if (ctx.operation === "edit" && !isEdit) throw new Error("image is required");
      return {
        kind: "submit",
        model: model,
        action: isEdit ? "image_to_image" : "text_to_image",
        requestBody: requestBody,
      };
    },
    render: function (ctx, task) {
      const data = plainObject(task && task.data);
      const urls = extractImages(data);
      const out = [];
      for (let i = 0; i < urls.length; i++) {
        const artifact = ctx && ctx.artifacts && ctx.artifacts["image-" + (i + 1)];
        const resolved = trimmed(artifact && artifact.url) || urls[i];
        out.push(imageDatum(resolved));
      }
      return { created: Number(task.created_at || 0), data: out, metadata: data };
    },
  },
  openai_responses: {
    decodeRequest: function (ctx) {
      if (!ctx.body || ctx.body.kind !== "json") throw new Error("JSON body required");
      const req = ctx.body.value;
      if (!req || typeof req !== "object" || Array.isArray(req)) throw new Error("request body must be an object");
      const model = trimmed(ctx.model);
      if (!model) throw new Error("model is required");
      if (req.input !== undefined && typeof req.input !== "string" && !Array.isArray(req.input)) {
        throw new Error("input must be a string or array");
      }

      const input = responsesInput(req);
      const requestBody = { model: model };
      const prompt = input.prompt || trimmed(req.prompt);
      if (prompt) requestBody.prompt = prompt;

      if (isNonEmptyMedia(req.image)) requestBody.image = req.image;
      if (isNonEmptyMedia(req.input_reference)) requestBody.input_reference = req.input_reference;

      const images = [];
      for (const item of [].concat(Array.isArray(req.images) ? req.images : [], input.images)) {
        if (isNonEmptyMedia(item) && !images.includes(item)) images.push(item);
      }
      if (images.length) requestBody.images = images;

      if (input.videos.length && !req.video && !req.reference_videos) {
        const spec = specFor(trimmed(ctx.upstreamModel) || model);
        if (spec && (spec.kind === "flashvsr" || spec.kind === "animate")) {
          requestBody.video = input.videos[0];
        } else {
          requestBody.reference_videos = input.videos;
        }
      }
      if (input.audios.length && !req.reference_audios) {
        requestBody.reference_audios = input.audios;
      }

      for (const field of FORWARDED_FIELDS) {
        if (field === "images") continue;
        if (hasOwn(req, field)) requestBody[field] = req[field];
      }

      const hasImage = Boolean(requestBody.image || requestBody.input_reference || requestBody.first_frame || images.length || (requestBody.reference_images || []).length);
      const action = modelIsImage(trimmed(ctx.upstreamModel) || model)
        ? hasImage
          ? "image_to_image"
          : "text_to_image"
        : hasImage || requestBody.video || (requestBody.reference_videos || []).length
          ? "image_to_video"
          : "text_to_video";

      return { kind: "submit", model: model, action: action, requestBody: requestBody };
    },
    renderEvents: function (ctx, task, previousState) {
      const status = String(task.status || "UNKNOWN").toUpperCase();
      const value = Number(String(task.progress || "").replace("%", ""));
      const progress = Number.isFinite(value) && value >= 0 && value <= 100 ? value : null;
      const state = { status: status, progress: progress };
      if (status === "SUCCESS") {
        const text = responsesOutputText(ctx, task);
        const events = previousState && previousState.status === status ? [] : text ? [{ type: "output", data: text }] : [];
        return { events: events, state: state, done: true };
      }
      if (status === "FAILURE") {
        return { events: [{ type: "error", code: "task_failed", message: trimmed(task.fail_reason) || "task failed" }], state: state, done: true };
      }
      if (previousState && previousState.status === status && previousState.progress === progress) {
        return { events: [], state: state, done: false };
      }
      const event = { type: "progress", message: status.toLowerCase() };
      if (progress !== null) event.progress = progress;
      return { events: [event], state: state, done: false };
    },
    renderFinal: function (ctx, task) {
      return {
        output: [
          {
            type: "message",
            status: "completed",
            role: "assistant",
            content: [
              {
                type: "output_text",
                text: responsesOutputText(ctx, task),
                annotations: [],
                logprobs: [],
              },
            ],
          },
        ],
        metadata: { vendor: "mulerouter" },
      };
    },
  },
};
