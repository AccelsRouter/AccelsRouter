// Fork: the asynchronous video interfaces and the speech synthesis of Baidu's
// model service (the AIGC endpoints of its VOD product), the task part beside
// the provider plugin baidu-vod. Baidu resells other vendors' models: each
// interface is a Baidu address prefix followed by the vendor's own protocol,
// with the API key as a Bearer token. A client calls the gateway at the
// vendor's own path and gets the vendor's own reply, or calls /v1/videos, the
// OpenAI video protocol, for any of the video models.
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
//   Kling video effects   {Base URL}/v3/aigc/kl + /videos/effects
//   Vidu          {Base URL}/v3/aigc/vd + Vidu paths
//   PixVerse      {Base URL}/v3/aigc/pv + /video/{endpoint}/generate,
//                 extend and fusion among the endpoints
//   HappyHorse    {Base URL}/v3/aigc/bailian + DashScope paths
//   MiniMax-H3    {Base URL}/v3/aigc/minimax + /v2 paths
//   Hailuo        {Base URL}/v3/aigc/minimax + /v1 paths
//   Speech        {Base URL}/v2/mini/t2a_v2, /v2/mini/t2a_async_v2 with
//                 /v2/task/{id}, and /v2/mini/voice_design, signed with an
//                 AccessKey

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
// The text-to-video aspect ratios of each API; the OpenAI video protocol sends
// the one nearest the request's size.
const KLING_PATH_ASPECT_RATIOS = ["16:9", "9:16", "1:1", "21:9"];
const KLING_BODY_ASPECT_RATIOS = ["16:9", "9:16", "1:1"];
// The models Kling serves on its omni-video endpoint alone.
const KLING_OMNI_MODELS = ["kling-v3-omni", "kling-video-o1"];
// Whether an element is a video character is not in the request, so an element
// counts as an input video.
const KLING_VIDEO_INPUTS = ["feature_video", "base_video", "element", "video"];
// Kling would call these back with its own task ids, which the gateway does
// not hand out.
const KLING_UNSUPPORTED_FIELDS = ["callback_url", "external_task_id"];

// Video effects have no model in Kling's request; the gateway routes and
// prices them under this name. Baidu's effects list gives what each
// effect_scene deducts, in the units Kling reports as final_unit_deduction: an
// amount of Baidu's own unit, not a currency price. A scene the list lacks
// reserves the dearest.
const KLING_EFFECTS_MODEL = "kling-effects";
const KLING_EFFECT_UNITS = {};
for (const group of [
  [1.5, "pet_moto_rider running_man pet_dance swing_swing day_to_night surfsurf skateskate horse_mask ornament_crash heart_gesture_pro kiss_pro fight_pro hug_pro venomous_spider the_high_kick zoom_out santa_random_surprise dollar_rain_pro pet_bee_pro thumbs_up_pro tiger_hug_pro pet_lion_pro"],
  [1.6, "ring_in_new new_year_greeting lion_dance lucky_red_packet lucky_money_come lion_dance_pet pet_red_packet"],
  [1.7, "3d_cartoon_2 winter_charm maestro_of_magic sparkler_wand snow_night_kiss eternal_kiss lantern_festival_cuju fortune_knocks_cartoon christmas_photo_shoot spark_in_the_snow scarlet_and_snow glamour_photo_shoot box_of_joy santa_gift steampunk_christmas snowglobe"],
  [2, "shooting_star send_my_love the_exercise ghost_step_dance poping santa_express"],
  [2.5, "twist_shake smooth_transition tangyuan_to_animal overdrive subject_3_dance unique_firework fortune_god_transform new_year_feast great_success horse_year_firework red_packet_box lucky_horse_year dumpling_making_pet fish_making_pet lantern_glow jumping_ginger_joy snowboarding bullet_time_360 surprise_bouquet bouquet_drop pure_white_wings black_wings golden_wing pink_pink_wings swish_swish cheers_2026 magic_match_tree"],
  [2.7, "flash_ride magic_carpet_ride good_luck_spirit dirt_rush return_of_the_king dance_with_dragon prosperity crystal_horse my_santa_pic"],
  [3, "football_dance finger_swipe palm_sized_figure_pro instant_christmas coronation_of_frost building_sweater birthday_star witch_transform running countdown_teleport first_toast_of_the_year vampire_transform pumpkin_head_transform demon_transform mummy_transform zombie_transform halloween_escape"],
  [3.5, "throne_of_king guardian_spirit jealousy_sway soccer_star make_it_rain luminous_elf woodland_elf wealth_drive"],
  [4, "african_sway shrug_dance bra_hot_dance samba_fever football_live football_live_9_16 spielberg_transition pet_skateboard sovereign_scepter minimalist_light perler_beads spring_bloom color_mixing heart_gesture_dance advent_of_flora motorcycle_dance city_to_village martial_arts bullet_time_lite happy_birthday landmark_reveal daily_ootd phantom_jewel cute_pumpkin_transform"],
  [4.5, "football_dance_2 air_dunk raid_check tiny_beast_printer prank_box shush_my_dreams bouncy_dance chanel_dance nezha"],
  [5, "pet_working_daily fra_hot_dance victory_slide sassy_shake the_hip_sway lucky_veggie yangge_dance daoma_dance expression_challenge hot_feet_dance f1_live tennis_trend tennis_trend_9_16 funky_martian"],
  [5.5, "switch_to_silk love_theme_song pumpitup_dance bloodline_dance swag_dance pigeon_dance"],
  [6, "pet_roll_window arg_hot_dance toss_run knock_at_a_door_revenge cute_dance"],
  [6.5, "good_luck_dance laicai_dance"],
  [7, "fortune_in_motion sedan_chair_dance drunk_dance drunk_dance_pet smooth_sailing_dance"],
  [7.2, "wiggle_faces_pets"],
  [7.5, "magic_world_vlog magic_world_vlog_9_16 chinese_trend"],
  [8, "critter_paste korean_baseball korean_baseball_9_16 bra_goal arg_goal por_goal red_card_sent_off martial_meow studio_look"],
  [8.4, "bottle_transform"],
  [8.5, "makeup_style get_rich_quick flash_drive"],
  [9, "paw_headbang pet_diving pet_fitness whirling_beverage whirling_beverage_9_16 french_elegance"],
  [9.5, "furry_birthday"],
  [10.4, "pet_press_conference"],
  [10.5, "hair_dressup"],
  [11, "dj_pet"],
  [11.2, "pet_train pet_crash_earth"],
  [11.5, "disco_paws"],
  [12, "bicycle_kick"],
]) {
  for (const scene of group[1].split(" ")) KLING_EFFECT_UNITS[scene] = group[0];
}
const KLING_DEAREST_EFFECT_UNITS = Object.keys(KLING_EFFECT_UNITS).reduce((dearest, scene) => Math.max(dearest, KLING_EFFECT_UNITS[scene]), 0);

function klingBilledInUnits(model) {
  return model === "kling-4.0" || model === KLING_EFFECTS_MODEL || Object.prototype.hasOwnProperty.call(KLING_UNIT_MODELS, model);
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
// The defaults of Vidu's current models with every resolution, for a request
// of the OpenAI video protocol whose model the endpoint does not list.
const VIDU_WIDEST = { durations: [1, 16, 5], resolutions: ["720p", "360p", "540p", "1080p", "2K", "4K"] };
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
// Extending a video and generating from several subjects, with the models
// Baidu lists for each. Baidu's price list puts extending in the generation
// tables, by the requested duration, and fusion in a list of its own; the
// operation fact tells the three apart for a price that needs it. Baidu's
// name for PixVerse's c1 model is not known, so it is not served.
const PIXVERSE_EDIT_ENDPOINTS = {
  extend: { action: "video_extend", models: ["pixverse4.5", "pixverse5.0", "pixverse5.5", "pixversev5.6", "pixversev6.0"] },
  fusion: { action: "reference_to_video", models: ["pixverse4.5", "pixverse5.0", "pixverse5.5", "pixversev5.6"] },
};
const PIXVERSE_QUALITIES = ["360p", "540p", "720p", "1080p"];
const PIXVERSE_MAX_SECONDS = 15;
// What the OpenAI video protocol sends for a request that names no duration,
// size or resolution, and the text-to-video aspect ratios it picks from.
const PIXVERSE_DEFAULT_SECONDS = 5;
const PIXVERSE_DEFAULT_QUALITY = "720p";
const PIXVERSE_ASPECT_RATIOS = ["16:9", "4:3", "1:1", "3:4", "9:16"];
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
  operation: {
    enum: ["generate", "extend", "fusion"],
    enumLabels: {
      generate: { en: "Text, image or frames to video", zh: "文/图/首尾帧生视频" },
      extend: { en: "Video extension", zh: "视频延长" },
      fusion: { en: "Multi-subject reference", zh: "多主体参考" },
    },
    description: { en: "Generation type", zh: "生成类型" },
  },
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
const HAPPYHORSE_ASPECT_RATIOS = ["16:9", "9:16", "1:1", "4:3", "3:4"];
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
// The OpenAI video protocol sends 5 seconds for a request that names none,
// and checks the content it assembles as the hailuo plugin does.
const H3_DEFAULT_DURATION = 5;
const H3_MAX_FRAME_IMAGES = 2;
const H3_MAX_REFERENCE_VIDEOS = 3;
const H3_MAX_REFERENCE_AUDIOS = 3;
const H3_USAGE_SCHEMA = {
  seconds: SECONDS_FIELD,
  resolution: { enum: ["768P", "2K"], description: RESOLUTION_DESCRIPTION },
  input_images: { type: "number", unit: "count", description: { en: "Input image unit price", zh: "输入图片单价" } },
  input_video_seconds: { type: "number", unit: "second", description: { en: "Input video unit price", zh: "输入视频单价" } },
};
const H3_MAX_USAGE_SCHEMA = { seconds: SECONDS_FIELD, resolution: resolutionField(["480P", "768P"]) };

// ---------------------------------------------------------------------------
// MiniMax's /v1 video API, which Baidu serves for three Hailuo models. They
// are the hailuo plugin's, so its usage facts, checks and defaults apply:
// billed per video by duration (6 or 10 seconds) and resolution.
// ---------------------------------------------------------------------------
const HAILUO_MODELS = ["MiniMax-Hailuo-2.3", "MiniMax-Hailuo-2.3-Fast", "MiniMax-Hailuo-02"];
const HAILUO_23_USAGE_SCHEMA = { seconds: SECONDS_FIELD, resolution: { enum: ["768P", "1080P"], description: RESOLUTION_DESCRIPTION } };
const HAILUO_02_USAGE_SCHEMA = { seconds: SECONDS_FIELD, resolution: { enum: ["512P", "768P", "1080P"], description: RESOLUTION_DESCRIPTION } };
// MiniMax's own fields that the OpenAI video protocol carries in metadata.
const HAILUO_METADATA_FIELDS = ["prompt_optimizer", "fast_pretreatment", "aigc_watermark", "first_frame_image", "last_frame_image", "subject_reference"];

// ---------------------------------------------------------------------------
// Speech synthesis: MiniMax's t2a_v2, billed per character, a Chinese
// character counting two.
// ---------------------------------------------------------------------------
const SPEECH_MODELS = ["speech-2.8-hd", "speech-2.8-turbo", "speech-2.6-hd", "speech-2.6-turbo", "speech-02-hd", "speech-02-turbo", "speech-01-hd", "speech-01-turbo"];
const SPEECH_MAX_CHARACTERS = 10000;
// The asynchronous interface takes longer texts, counted the same way.
const SPEECH_ASYNC_MAX_CHARACTERS = 50000;
const SPEECH_USAGE_SCHEMA = {
  characters: { type: "number", unit: "token", description: { en: "Speech synthesis unit price", zh: "语音合成单价" } },
};
// Voice design has no model in MiniMax's request; the gateway routes and
// prices it under this name. Baidu charges for a designed voice when it is
// first used in a synthesis, which the gateway cannot see, so each voice is
// billed when it is made, with the characters of its preview text.
const VOICE_DESIGN_MODEL = "minimax-voice-design";
const VOICE_DESIGN_MAX_PREVIEW_CHARACTERS = 500;
const VOICE_DESIGN_USAGE_SCHEMA = {
  voices: { type: "number", unit: "count", unitLabel: { en: "voice", zh: "个" }, description: { en: "Voice creation unit price", zh: "音色创建单价" } },
  characters: { type: "number", unit: "token", description: { en: "Preview synthesis unit price", zh: "试听合成单价" } },
};

const VIDEO_MODELS = Object.keys(SEEDANCE_MODELS).concat(Object.keys(KLING_PATH_MODELS), KLING_BODY_MODELS, VIDU_MODELS, Object.keys(PIXVERSE_MODELS), HAPPYHORSE_MODELS, [H3_MODEL, H3_MAX_MODEL], HAILUO_MODELS);
// The video models a vendor's plugin (doubao, kling, vidu, hailuo) declares
// too. On /v1/videos the host has every plugin that declares a model decode
// the request before it picks a channel, and the plugin of that channel
// executes it, so these models are read here exactly as the vendor's plugin
// reads them: a client must not see which channel served it.
const SHARED_VIDEO_MODELS = Object.keys(SEEDANCE_MODELS).concat(
  ["kling-4.0", "kling-v2-5-turbo", "kling-v2-6", "kling-v3"],
  VIDU_MODELS.filter((model) => model !== "viduq4"),
  [H3_MODEL],
  HAILUO_MODELS
);

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
    en: "Seedance, Kling, Vidu, PixVerse, HappyHorse and MiniMax video generation, Kling video effects, and MiniMax speech synthesis and voice design through Baidu's model service, in each vendor's own format or the OpenAI video format",
    zh: "通过百度模型服务调用 Seedance、可灵、Vidu、PixVerse、HappyHorse、MiniMax 视频生成、可灵视频特效，以及 MiniMax 语音合成与音色设计，使用各厂商自己的接口格式或 OpenAI 视频格式",
  },
  version: "1.2.0",
  author: { name: "AccelsRouter" },
  baseUrl: "https://vod.bj.baidubce.com",
  allowedHosts: ["vod.gz.baidubce.com"],
  keyHint: {
    en: "API Key, or API Key|AccessKey|SecretKey to also serve speech synthesis and voice design",
    zh: "API Key；需要语音合成与音色设计时填 API Key|AccessKey|SecretKey",
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
  models: VIDEO_MODELS.concat([KLING_EFFECTS_MODEL], SPEECH_MODELS, [VOICE_DESIGN_MODEL]),
  fetchMode: "per_task",
  // /v1/videos for the video models; video effects, speech synthesis and
  // voice design have no host protocol.
  protocols: [{ name: "openai_video", models: VIDEO_MODELS }],
  // The fallback of a channel alias that resolves to several models.
  usageSchema: { seconds: SECONDS_FIELD },
  usageProfiles: Object.keys(SEEDANCE_MODELS)
    .map((model) => ({ models: [model], schema: seedanceUsageSchema(SEEDANCE_MODELS[model]), examples: seedanceUsageExamples(SEEDANCE_MODELS[model]) }))
    .concat([
      { models: Object.keys(KLING_PATH_MODELS).concat(KLING_BODY_MODELS, [KLING_EFFECTS_MODEL]).filter(klingBilledInUnits), schema: KLING_UNITS_SCHEMA },
      { models: Object.keys(KLING_PATH_MODELS).concat(KLING_BODY_MODELS).filter((model) => !klingBilledInUnits(model)), schema: KLING_SECONDS_SCHEMA },
      { models: VIDU_MODELS, schema: VIDU_USAGE_SCHEMA },
      { models: Object.keys(PIXVERSE_MODELS), schema: PIXVERSE_USAGE_SCHEMA },
      { models: HAPPYHORSE_MODELS, schema: HAPPYHORSE_USAGE_SCHEMA },
      { models: [H3_MODEL], schema: H3_USAGE_SCHEMA },
      { models: [H3_MAX_MODEL], schema: H3_MAX_USAGE_SCHEMA },
      { models: ["MiniMax-Hailuo-2.3", "MiniMax-Hailuo-2.3-Fast"], schema: HAILUO_23_USAGE_SCHEMA },
      { models: ["MiniMax-Hailuo-02"], schema: HAILUO_02_USAGE_SCHEMA },
      { models: SPEECH_MODELS, schema: SPEECH_USAGE_SCHEMA, examples: [{ label: "1000 字符", facts: { characters: 1000 } }] },
      { models: [VOICE_DESIGN_MODEL], schema: VOICE_DESIGN_USAGE_SCHEMA, examples: [{ label: "1 个音色 · 100 字符试听", facts: { voices: 1, characters: 100 } }] },
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
    .concat([
      { method: "POST", path: "/v1/videos/effects", type: "submit", decode: "createKlingEffect", render: "klingCreated" },
      { method: "GET", path: "/v1/videos/effects/:task_id", type: "query", render: "klingStatus" },
    ])
    .concat(Object.keys(VIDU_ENDPOINTS).map((endpoint) => ({ method: "POST", path: "/ent/v2/" + endpoint, type: "submit", decode: "createViduTask", render: "viduCreated" })))
    .concat([{ method: "GET", path: "/ent/v2/tasks/:task_id/creations", type: "query", render: "viduStatus" }])
    // PixVerse's own paths. Lip sync is not served.
    .concat(
      Object.keys(PIXVERSE_ENDPOINTS)
        .concat(Object.keys(PIXVERSE_EDIT_ENDPOINTS))
        .map((endpoint) => ({ method: "POST", path: "/openapi/v2/video/" + endpoint + "/generate", type: "submit", decode: "createPixverseVideo", render: "pixverseCreated" }))
    )
    .concat([
      { method: "GET", path: "/openapi/v2/video/result/:task_id", type: "query", render: "pixverseStatus" },
      { method: "POST", path: "/api/v1/services/aigc/video-generation/video-synthesis", type: "submit", decode: "createHappyHorseTask", render: "happyHorseCreated" },
      { method: "GET", path: "/api/v1/tasks/:task_id", type: "query", render: "happyHorseStatus" },
      { method: "POST", path: "/v2/video_generation", type: "submit", decode: "createH3Task", render: "h3Created" },
      { method: "GET", path: "/v2/query/video_generation/:task_id", type: "query", render: "h3Status" },
      // MiniMax's /v1 video paths, which the hailuo plugin declares too. The
      // query reads the task id from the query string, as MiniMax does, and
      // the video, or the audio of an asynchronous synthesis, is retrieved
      // under the file id the query reports.
      { method: "POST", path: "/v1/video_generation", type: "submit", decode: "createHailuoTask", render: "h3Created", models: HAILUO_MODELS },
      { method: "GET", path: "/v1/query/video_generation", type: "query", taskIdParam: "task_id", render: "hailuoStatus" },
      { method: "GET", path: "/v1/files/retrieve", type: "query", taskIdParam: "file_id", render: "minimaxFile" },
      // MiniMax answers with the audio; there is no task to query again.
      { method: "POST", path: "/v1/t2a_v2", type: "submit", decode: "createSpeech", render: "speechCreated", retainResult: false },
      // MiniMax's asynchronous synthesis and its query, answered in MiniMax's
      // structure from Baidu's task reply.
      { method: "POST", path: "/v1/t2a_async_v2", type: "submit", decode: "createAsyncSpeech", render: "asyncSpeechCreated" },
      { method: "GET", path: "/v1/query/t2a_async_query_v2", type: "query", taskIdParam: "task_id", render: "asyncSpeechStatus" },
      // MiniMax answers with the voice and its preview audio at once.
      { method: "POST", path: "/v1/voice_design", type: "submit", decode: "createVoiceDesign", render: "speechCreated", retainResult: false },
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

function klingDuration(value, shortest, longest, refusal) {
  const n = typeof value === "string" && value.trim() !== "" ? Number(value) : value;
  if (!Number.isInteger(n) || n < shortest || n > longest) throw new Error(refusal || "duration must be an integer from " + shortest + " to " + longest);
  return n;
}

// What a request of the API that takes the model in its path may not carry:
// Kling would call back, or look tasks up, by ids the gateway does not hand
// out, and the duration is billed, so it is kept within the model's range.
function checkKlingPathBody(model, body) {
  const options = plainObject(body.options);
  for (const name of KLING_UNSUPPORTED_FIELDS) {
    if (options[name] !== undefined && options[name] !== null && options[name] !== "") throw new Error("options." + name + " is not supported; query the task instead");
  }
  const settings = plainObject(body.settings);
  if (settings.duration !== undefined && settings.duration !== null) klingDuration(settings.duration, KLING_PATH_MIN_SECONDS, KLING_PATH_MODELS[model].maxSeconds);
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

// The units Baidu's effects list gives an effect_scene, which it once writes
// with a space, or the dearest of the list for one it lacks.
function klingEffectUnits(scene) {
  const key = trimmed(scene).replace(/\s+/g, "");
  return Object.prototype.hasOwnProperty.call(KLING_EFFECT_UNITS, key) ? KLING_EFFECT_UNITS[key] : KLING_DEAREST_EFFECT_UNITS;
}

// The usage of a Kling request: units for the models the kling plugin also
// serves, seconds with their conditions for the others.
function klingUsage(ctx, req) {
  const body = req.vendorBody;
  if (req.api === "effects") return { units: klingEffectUnits(body.effect_scene) };
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

function pixverseUsage(body, endpoint) {
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
    operation: endpoint === "extend" || endpoint === "fusion" ? endpoint : "generate",
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

// ------------------------------ Hailuo, /v1 --------------------------------
// The hailuo plugin's reading of MiniMax's /v1 video API, in its words.

// The resolution a size or a resolution names; the three models render 768P
// where others render 720P.
function hailuoResolution(size, model) {
  const value = String(size || "");
  if (value.includes("1080")) return "1080P";
  if (value.includes("768")) return "768P";
  if (value.includes("720")) return HAILUO_MODELS.includes(model) ? "768P" : "720P";
  if (value.includes("512")) return "512P";
  return HAILUO_MODELS.includes(model) ? "768P" : "720P";
}

// The resolution a request of the OpenAI video protocol names: its
// resolution, metadata's, or its size, and otherwise the model's default.
function hailuoProtocolResolution(req, model) {
  const metadata = req.metadata || {};
  return hailuoResolution(req.resolution || metadata.resolution || req.size, model);
}

// MiniMax's frame and subject fields mark a request as image-to-video.
function hailuoHasImage(body) {
  return Boolean(body.first_frame_image || body.last_frame_image || body.subject_reference);
}

// The durations and resolutions MiniMax renders, checked before a request is
// reserved: 6 or 10 seconds, 10 only at 768P (or 512P, which only
// MiniMax-Hailuo-02's image-to-video renders), and no text-to-video on
// MiniMax-Hailuo-2.3-Fast. A native request leaves its resolution to MiniMax
// and passes none.
function checkHailuoRequest(model, duration, resolution, hasImage) {
  if (model === "MiniMax-Hailuo-2.3-Fast" && !hasImage) throw new Error("MiniMax-Hailuo-2.3-Fast supports image-to-video only");
  if (!HAILUO_MODELS.includes(model)) {
    if (duration !== undefined && Number(duration) !== 6) throw new Error(model + " duration must be 6");
    return;
  }
  const seconds = duration === undefined ? 6 : Number(duration);
  if (seconds !== 6 && seconds !== 10) throw new Error(model + " duration must be 6 or 10");
  if (resolution === undefined) return;
  const lowest = model === "MiniMax-Hailuo-02" && hasImage;
  if (seconds === 10) {
    if (lowest && resolution !== "768P" && resolution !== "512P") throw new Error("MiniMax-Hailuo-02 duration 10 only allows resolution 768P or 512P");
    if (!lowest && resolution !== "768P") throw new Error(model + " duration 10 only allows resolution 768P");
    return;
  }
  const allowed = lowest ? ["512P", "768P", "1080P"] : ["768P", "1080P"];
  if (!allowed.includes(resolution)) throw new Error(model + " duration 6 only allows resolution " + allowed.join(" or "));
}

// The usage of a native /v1 request: MiniMax's default of 6 seconds for a
// duration the body leaves out, and for the resolution the body's own when
// the model renders it, the model's default when it names none, and
// otherwise the model's dearest. The two input facts are MiniMax-H3's, which
// the hailuo plugin reports as zero for these models.
function hailuoUsage(model, body) {
  const hasImage = hailuoHasImage(body);
  checkHailuoRequest(model, body.duration === undefined || body.duration === null ? undefined : Number(body.duration), undefined, hasImage);
  let resolutions = ["512P", "720P", "768P", "1080P"];
  if (HAILUO_MODELS.includes(model)) resolutions = model === "MiniMax-Hailuo-02" && hasImage && !body.last_frame_image ? ["512P", "768P", "1080P"] : ["768P", "1080P"];
  let resolution = trimmed(body.resolution).toUpperCase();
  if (!resolutions.includes(resolution)) resolution = (body.resolution === undefined || body.resolution === null) && HAILUO_MODELS.includes(model) ? "768P" : resolutions[resolutions.length - 1];
  const seconds = Number(body.duration);
  return { seconds: Number.isFinite(seconds) && seconds > 0 ? seconds : 6, resolution: resolution, input_images: 0, input_video_seconds: 0 };
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

  // Kling's own effects body, sent as is for Kling to check. The effect_scene
  // sets what is deducted, so it must be named.
  createKlingEffect: function (ctx) {
    const body = jsonBody(ctx);
    if (typeof body.effect_scene !== "string" || !body.effect_scene.trim()) throw new Error("effect_scene is required");
    for (const name of KLING_UNSUPPORTED_FIELDS) {
      if (body[name] !== undefined && body[name] !== null && body[name] !== "") throw new Error(name + " is not supported; query the task instead");
    }
    return { kind: "submit", model: KLING_EFFECTS_MODEL, action: "image_to_video", requestBody: { vendor: "kling", api: "effects", model: KLING_EFFECTS_MODEL, endpoint: "effects", vendorBody: body } };
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
    const endpoint = lastSegment(ctx.path, 1);
    pixverseUsage(body, endpoint);
    // Extending and fusion are priced for the models Baidu lists for them.
    const edit = PIXVERSE_EDIT_ENDPOINTS[endpoint];
    if (edit && !edit.models.includes(model)) throw new Error("model must be one of " + edit.models.map((name) => PIXVERSE_MODELS[name]).join(", ") + " on this endpoint");
    // A Baidu media id names a video anywhere in the channel's Baidu account,
    // so a customer could extend another customer's video by its id: the
    // source is only taken as a URL.
    if (endpoint === "extend" && body.video_media_id !== undefined) throw new Error("video_media_id is not supported; send source_video_url");
    return { kind: "submit", model: model, action: edit ? edit.action : PIXVERSE_ENDPOINTS[endpoint], requestBody: { vendor: "pixverse", model: model, endpoint: endpoint, vendorBody: body } };
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

  // MiniMax's /v1 body, sent as is. The checks of the model the channel sends
  // run when the request is built; MiniMax checks the rest.
  createHailuoTask: function (ctx) {
    const body = jsonBody(ctx);
    const model = trimmed(body.model);
    if (!model) throw new Error("model is required");
    if (body.callback_url !== undefined && body.callback_url !== null) throw new Error("callback_url is not supported; query the task instead");
    return { kind: "submit", model: model, action: hailuoHasImage(body) ? "image_to_video" : "text_to_video", requestBody: { vendor: "minimax", api: "v1", model: model, vendorBody: body } };
  },
  // The /v1 query reply. Before the first poll the task holds the create
  // reply, which has no status. A finished task names its video by a file id,
  // as MiniMax does: the task id, which /v1/files/retrieve takes. A file id
  // Baidu reports is its own and is replaced, as the hailuo plugin replaces
  // MiniMax's.
  hailuoStatus: function (ctx, task) {
    const data = plainObject(task.data);
    const output = withTaskId(data, "task_id", task.task_id);
    if (trimmed(output.file_id) || (task.status === "SUCCESS" && (ctx.artifacts || {}).video)) output.file_id = task.task_id;
    if (task.status === "FAILURE") output.status = "Fail";
    else if (typeof data.status !== "string") output.status = task.status === "IN_PROGRESS" ? "Processing" : "Queueing";
    return output;
  },
  // MiniMax's file reply for the file id a query reports: download_url is the
  // gateway's own address of the video or the audio, which takes no
  // credentials.
  minimaxFile: function (ctx, task) {
    const data = plainObject(task.data);
    const speech = data.type === "TTS";
    const artifact = (ctx.artifacts || {})[speech ? "audio" : "video"];
    if (!artifact || data.status !== (speech ? "SUCCESS" : "Success")) throw new Error("artifact_not_found");
    const extension = speech ? /\.([a-z0-9]+)$/i.exec(trimmed(plainObject(data.ttsTaskInfo).url).split("?")[0]) : null;
    const file = {
      file_id: task.task_id,
      filename: task.task_id + (speech ? (extension ? "." + extension[1] : "") : ".mp4"),
      purpose: speech ? "t2a_async_output" : "video_generation",
      download_url: artifact.url,
    };
    if (Number(task.finished_at) > 0) file.created_at = Number(task.finished_at);
    return { file: file, base_resp: { status_code: 0, status_msg: "success" } };
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
  // MiniMax's own t2a_async_v2 body. The text is billed, so it must be in the
  // request, where it can be counted, and not in a file.
  createAsyncSpeech: function (ctx) {
    const body = jsonBody(ctx);
    const model = trimmed(body.model);
    if (!model) throw new Error("model is required");
    if (body.text_file_id !== undefined && body.text_file_id !== null) throw new Error("text_file_id is not supported; send the text itself");
    if (typeof body.text !== "string" || !body.text.trim()) throw new Error("text is required");
    if (speechCharacters(body.text) > SPEECH_ASYNC_MAX_CHARACTERS) throw new Error("text must be at most " + SPEECH_ASYNC_MAX_CHARACTERS + " characters, a Chinese character counting two");
    return { kind: "submit", model: model, action: "text_to_speech", requestBody: { vendor: "speech", api: "async", model: model, vendorBody: body } };
  },
  asyncSpeechCreated: function (ctx, task) {
    const data = plainObject(task.data);
    return { task_id: task.task_id, usage_characters: data.usage_characters, base_resp: data.base_resp || { status_code: 0, status_msg: "OK" } };
  },
  // MiniMax's query reply of a synthesis, from the status of the task: before
  // the first poll the task holds the create reply, and the gateway can fail
  // a task Baidu still reports as running. A finished synthesis names its
  // audio by a file id, the task id, which /v1/files/retrieve takes.
  asyncSpeechStatus: function (ctx, task) {
    if (plainObject(task.data).type !== "TTS") throw new Error("artifact_not_found");
    const statuses = { SUCCESS: "Success", FAILURE: "Failed" };
    const output = { task_id: task.task_id, status: statuses[task.status] || "Processing" };
    if (task.status === "SUCCESS" && (ctx.artifacts || {}).audio) output.file_id = task.task_id;
    output.base_resp = { status_code: 0, status_msg: "success" };
    return output;
  },
  // MiniMax's own voice_design body. The preview text is billed, so its
  // length is bounded first; MiniMax checks the rest.
  createVoiceDesign: function (ctx) {
    const body = jsonBody(ctx);
    if (typeof body.preview_text !== "string" || !body.preview_text.trim()) throw new Error("preview_text is required");
    if (Array.from(body.preview_text).length > VOICE_DESIGN_MAX_PREVIEW_CHARACTERS) throw new Error("preview_text must be at most " + VOICE_DESIGN_MAX_PREVIEW_CHARACTERS + " characters");
    return { kind: "submit", model: VOICE_DESIGN_MODEL, action: "voice_design", requestBody: { vendor: "speech", api: "design", model: VOICE_DESIGN_MODEL, vendorBody: body } };
  },

  // Each vendor's own error reply, by the path the client called.
  error: function (ctx, error) {
    const path = String(ctx.path || "");
    if (path.indexOf("/api/v3/") >= 0) return { error: { code: error.httpStatus === 400 ? "InvalidParameter" : "InternalServiceError", message: error.message } };
    if (path.indexOf("/openapi/v2/") >= 0) return { ErrCode: error.httpStatus, ErrMsg: error.message };
    if (path.indexOf("/api/v1/") >= 0) return { code: error.code, message: error.message, request_id: error.requestId || "" };
    if (path.indexOf("/ent/v2/") >= 0) return { code: error.code, message: error.message };
    if (path.indexOf("/v2/") >= 0) return { type: "error", error: { type: error.httpStatus >= 500 ? "server_error" : "bad_request_error", message: error.message, http_code: String(error.httpStatus) }, request_id: error.requestId || "" };
    // MiniMax's /v1 video API answers 200 with its status code in base_resp,
    // the codes the hailuo plugin answers with.
    if (["/v1/video_generation", "/v1/query/video_generation", "/v1/files/retrieve"].some((name) => path.indexOf(name) >= 0)) {
      const codes = { 400: 2013, 401: 1004, 403: 1004, 404: 2013, 413: 2013, 429: 1002 };
      return { __status: 200, __body: { base_resp: { status_code: codes[error.httpStatus] || (error.httpStatus >= 500 ? 1013 : 1000), status_msg: error.message } } };
    }
    if (["/t2a_v2", "/t2a_async_v2", "/t2a_async_query_v2", "/voice_design"].some((name) => path.indexOf(name) >= 0)) return { __status: 200, __body: { base_resp: { status_code: error.httpStatus >= 500 ? 1013 : 2013, status_msg: error.message } } };
    return { code: error.httpStatus >= 500 ? 5000 : 1200, message: error.message, request_id: error.requestId || "" };
  },
};

// The decoders of the Kling paths that take the model: the route names the
// model and the endpoint.
for (const route of klingPathRoutes) {
  native[route.decode] = function (ctx) {
    const body = jsonBody(ctx);
    checkKlingPathBody(route.model, body);
    return {
      kind: "submit",
      model: route.model,
      action: KLING_PATH_ACTIONS[route.endpoint],
      requestBody: { vendor: "kling", api: "path", model: route.model, endpoint: route.endpoint, vendorBody: body },
    };
  };
}

// ------------------------------- driver hooks -------------------------------

// The vendor request a task sends: a native route's own body, or the one a
// request of the OpenAI video protocol becomes.
function vendorRequest(ctx) {
  const req = ctx.requestBody;
  if (!req.protocol) return req;
  if (req.invalid) throw new Error(req.invalid);
  return VIDEO_PROTOCOL[req.vendor].request(ctx, req);
}

// A native request reaches Baidu as the client sent it, with the channel's
// model.
export function buildSubmitRequest(ctx) {
  const req = vendorRequest(ctx);
  const model = upstreamModel(ctx);
  const descriptor = { method: "POST", headers: bearerHeaders(ctx), action: req.action || ctx.action };
  // Reject what cannot be billed before any quota is reserved.
  extractUsage(ctx);
  if (req.vendor === "seedance") {
    descriptor.url = asyncBase(ctx) + "/async/seedance/api/v3/contents/generations/tasks";
    descriptor.body = Object.assign({}, req.vendorBody, { model: model });
  } else if (req.vendor === "kling" && req.api === "path") {
    descriptor.url = asyncBase(ctx) + "/async/kling/" + req.endpoint + "/" + model;
    descriptor.body = req.vendorBody;
  } else if (req.vendor === "kling" && req.api === "effects") {
    descriptor.url = ctx.baseUrl + "/v3/aigc/kl/videos/effects";
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
  } else if (req.vendor === "minimax" && req.api === "v1") {
    descriptor.url = ctx.baseUrl + "/v3/aigc/minimax/v1/video_generation";
    descriptor.body = Object.assign({}, req.vendorBody, { model: model });
  } else if (req.vendor === "minimax") {
    if (model !== H3_MODEL && model !== H3_MAX_MODEL) throw new Error(model + " is not served on /v2/video_generation");
    descriptor.url = ctx.baseUrl + "/v3/aigc/minimax/v2/video_generation";
    descriptor.body = Object.assign({}, req.vendorBody, { model: model });
  } else if (req.vendor === "speech") {
    const parts = keyParts(ctx);
    if (parts.length !== 3 || !parts[1] || !parts[2]) throw new Error("speech synthesis needs a channel key of the form API Key|AccessKey|SecretKey");
    descriptor.url = ctx.baseUrl + (req.api === "async" ? "/v2/mini/t2a_async_v2" : req.api === "design" ? "/v2/mini/voice_design" : "/v2/mini/t2a_v2");
    descriptor.headers = Object.assign({ "Content-Type": "application/json", Accept: "application/json" }, bceHeaders("POST", descriptor.url, parts[1], parts[2]));
    // A voice design names no model.
    descriptor.body = req.api === "design" ? req.vendorBody : Object.assign({}, req.vendorBody, { model: model });
  } else {
    throw new Error("unsupported request");
  }
  return descriptor;
}

// The plugin state names the vendor for the polls, which have no request, and
// keeps what settles a task whose reply does not report its usage.
export function parseSubmitResponse(ctx, resp) {
  const req = vendorRequest(ctx);
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
      state.facts = klingUsage(ctx, req);
      state.open = settings.duration === undefined || settings.duration === null;
      state.requested = state.open ? 0 : Number(settings.duration);
      state.total = profile.totalDuration === true;
    }
  } else if (req.vendor === "kling" && req.api === "effects") {
    if (body.code !== undefined && body.code !== 0 && body.code !== "0") throw new Error(trimmed(body.message) || "kling submit failed");
    // Baidu answers some of Kling's interfaces in its own structure, {taskId};
    // either way the task is queried at the interface's own path.
    taskId = trimmed(plainObject(body.data).task_id) || trimmed(body.taskId);
    state.api = "effects";
    state.endpoint = req.endpoint;
    state.reserved = klingUsage(ctx, req).units;
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
    if (req.api === "v1") state.api = "v1";
  } else if (req.vendor === "speech" && req.api === "async") {
    const base = plainObject(body.base_resp);
    if (base.status_code) throw new Error(trimmed(base.status_msg) || "speech synthesis failed");
    taskId = trimmed(body.task_id) || trimmed(body.taskId);
    if (!taskId) throw new Error(trimmed(body.message) || "missing task id");
    state.api = "async";
    // Baidu's estimate of the billed characters settles the task when it is
    // above the plugin's count and within what the text could count.
    const text = req.vendorBody.text;
    const estimate = body.usage_characters;
    if (Number.isInteger(estimate) && estimate > speechCharacters(text) && estimate <= 2 * Array.from(text).length) state.characters = estimate;
    // The status route tells a synthesis from the plugin's other tasks by
    // the type Baidu's task reply carries.
    return { taskId: taskId, taskData: Object.assign({}, body, { type: "TTS" }), state: state };
  } else if (req.vendor === "speech") {
    const base = plainObject(body.base_resp);
    if (base.status_code) throw new Error(trimmed(base.status_msg) || "speech synthesis failed");
    if (req.api === "design") {
      if (body.code || !trimmed(body.voice_id)) throw new Error(trimmed(body.message) || "voice design returned no voice");
    } else if (body.code || !trimmed(plainObject(body.data).audio)) throw new Error(trimmed(body.message) || "speech synthesis returned no audio");
    if (req.api) state.api = req.api;
    if (!trimmed(ctx.publicTaskId)) throw new Error("missing gateway task id");
    return { taskId: trimmed(ctx.publicTaskId), taskData: body, state: state, immediate: { status: "SUCCESS", progress: "100%" } };
  }
  if (!taskId) throw new Error("missing task id");
  return { taskId: taskId, taskData: body, state: state };
}

export function extractUsage(ctx) {
  if (ctx.usagePurpose === "billing_ratios") return null;
  const req = vendorRequest(ctx);
  const body = req.vendorBody;
  if (req.vendor === "seedance") return seedanceUsage(ctx, req.reserved || body);
  if (req.vendor === "kling") return klingUsage(ctx, req);
  if (req.vendor === "vidu") return viduUsage(req.endpoint, upstreamModel(ctx), body);
  if (req.vendor === "pixverse") return pixverseUsage(body, req.endpoint);
  if (req.vendor === "happyhorse") return happyHorseUsage(body);
  if (req.vendor === "minimax" && req.api === "v1") return req.facts || hailuoUsage(upstreamModel(ctx), body);
  if (req.vendor === "minimax") return h3Usage(upstreamModel(ctx), body);
  if (req.vendor === "speech" && req.api === "design") {
    if (typeof body.preview_text !== "string" || Array.from(body.preview_text).length > VOICE_DESIGN_MAX_PREVIEW_CHARACTERS) throw new Error("preview_text must be at most " + VOICE_DESIGN_MAX_PREVIEW_CHARACTERS + " characters");
    return { voices: 1, characters: speechCharacters(body.preview_text) };
  }
  if (req.vendor === "speech") {
    const longest = req.api === "async" ? SPEECH_ASYNC_MAX_CHARACTERS : SPEECH_MAX_CHARACTERS;
    const characters = speechCharacters(body.text);
    if (characters > longest) throw new Error("text must be at most " + longest + " characters, a Chinese character counting two");
    return { characters: characters };
  }
  throw new Error("unsupported request");
}

export function buildQueryRequest(ctx) {
  const state = plainObject(ctx.state);
  const id = encodeURIComponent(ctx.taskId);
  let url;
  if (state.vendor === "speech" && state.api === "async") {
    // Baidu's task query, signed as the synthesis was.
    const parts = keyParts(ctx);
    if (parts.length !== 3 || !parts[1] || !parts[2]) throw new Error("speech synthesis needs a channel key of the form API Key|AccessKey|SecretKey");
    url = ctx.baseUrl + "/v2/task/" + id;
    return { url: url, method: "GET", headers: Object.assign({ "Content-Type": "application/json", Accept: "application/json" }, bceHeaders("GET", url, parts[1], parts[2])) };
  }
  if (state.vendor === "seedance") url = asyncBase(ctx) + "/async/seedance/api/v3/contents/generations/tasks/" + id;
  else if (state.vendor === "kling" && state.api === "path") url = asyncBase(ctx) + "/async/kling/tasks?task_ids=" + id;
  else if (state.vendor === "kling") url = ctx.baseUrl + "/v3/aigc/kl/videos/" + state.endpoint + "/" + id;
  else if (state.vendor === "vidu") url = ctx.baseUrl + "/v3/aigc/vd/ent/v2/tasks/" + id + "/creations";
  // Baidu's PixVerse interface has no query of its own.
  else if (state.vendor === "pixverse" || state.detail) url = ctx.baseUrl + "/v3/tasks/" + id;
  else if (state.vendor === "happyhorse") url = ctx.baseUrl + "/v3/aigc/bailian/api/v1/tasks/" + id;
  else if (state.vendor === "minimax" && state.api === "v1") url = ctx.baseUrl + "/v3/aigc/minimax/v1/query/video_generation?task_id=" + id;
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
  if (state.vendor === "minimax" && state.api === "v1") {
    if (body.base_resp && body.base_resp.status_code !== 0) return { status: "FAILURE", progress: "100%", reason: body.base_resp.status_msg || "" };
    const statuses = { Preparing: "IN_PROGRESS", Queueing: "IN_PROGRESS", Processing: "IN_PROGRESS", Success: "SUCCESS", Fail: "FAILURE" };
    const status = statuses[body.status];
    if (!status) return { status: "UNKNOWN", reason: "unrecognized status: " + trimmed(body.status) };
    const result = { status: status, progress: status === "IN_PROGRESS" ? (body.status === "Processing" ? "50%" : "30%") : "100%", reason: status === "FAILURE" ? "task failed" : "" };
    // Baidu keeps the video and reports its address with the status.
    const url = trimmed(plainObject(body.file).download_url);
    if (status === "SUCCESS" && url) result.url = url;
    return result;
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
  if (state.vendor === "speech") {
    const info = plainObject(body.ttsTaskInfo);
    if (body.status === "RUNNING") return { status: "IN_PROGRESS", progress: "50%" };
    if (body.status === "SUCCESS") return { status: "SUCCESS", progress: "100%", url: trimmed(info.url) };
    if (body.status === "FAILED") return { status: "FAILURE", progress: "100%", reason: trimmed(info.errMsg) || "speech synthesis failed" };
    return { status: "UNKNOWN", reason: "unrecognized status: " + trimmed(body.status) };
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
  if (state.vendor === "kling" && state.api === "effects") {
    // The units Kling deducted, never more than the list gives the effect. A
    // reply without a deduction keeps the reservation.
    const units = Number.parseFloat(plainObject(body.data).final_unit_deduction);
    return units > 0 ? { units: Math.min(units, state.reserved) } : null;
  }
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
  if (state.vendor === "minimax" && state.api === "v1") {
    // MiniMax reports the size of the video, not its price tier.
    const width = Number(body.video_width || 0);
    const height = Number(body.video_height || 0);
    return width > 0 && height > 0 ? { resolution: hailuoResolution(width + "x" + height, "") } : null;
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
  if (state.vendor === "speech" && state.api === "async") return state.characters ? { characters: state.characters } : null;
  if (state.vendor === "speech" && state.api === "design") return null;
  if (state.vendor === "speech") {
    const characters = plainObject(body.extra_info).usage_characters;
    return Number.isInteger(characters) && characters >= 1 && characters <= SPEECH_MAX_CHARACTERS ? { characters: characters } : null;
  }
  return null;
}

// ------------------------------ video artifacts -----------------------------

// The video of a finished task, from the vendor's last reply.
function taskVideoURL(state, data) {
  if (state.detail || state.vendor === "pixverse") return taskDetailVideoURL(data, 0);
  if (state.vendor === "seedance") return trimmed(plainObject(data.content).video_url);
  if (state.vendor === "kling" && state.api === "path") {
    const videos = klingPathVideos(klingPathEntry(data));
    return videos.length ? trimmed(videos[0].url) : "";
  }
  if (state.vendor === "kling") {
    const videos = plainObject(plainObject(data.data).task_result).videos;
    return Array.isArray(videos) && videos.length ? trimmed(plainObject(videos[0]).url) : "";
  }
  if (state.vendor === "vidu") return Array.isArray(data.creations) && data.creations.length ? trimmed(plainObject(data.creations[0]).url) : "";
  if (state.vendor === "happyhorse") return trimmed(plainObject(data.output).video_url);
  if (state.vendor === "minimax" && state.api === "v1") return trimmed(plainObject(data.file).download_url);
  if (state.vendor === "minimax") {
    const task = h3Task(data);
    return task ? trimmed(plainObject(task.content).url) : taskDetailVideoURL(data, 0);
  }
  return "";
}

// The artifacts the vendors' plugins list for the same task: the video, and
// the last frame Ark returns with it when asked to.
export function listArtifacts(task) {
  if (task.status !== "SUCCESS") return [];
  const state = plainObject(task.state);
  const data = plainObject(task.data);
  const artifacts = [];
  // The audio of an asynchronous synthesis.
  if (state.vendor === "speech" && trimmed(plainObject(data.ttsTaskInfo).url)) artifacts.push({ key: "audio", type: "audio" });
  if (taskVideoURL(state, data)) artifacts.push(state.vendor === "minimax" ? { key: "video", type: "video", mimeType: "video/mp4" } : { key: "video", type: "video" });
  if (state.vendor === "seedance" && trimmed(plainObject(data.content).last_frame_url)) artifacts.push({ key: "last_frame", type: "image", mimeType: "image/png" });
  return artifacts;
}

// The vendors' result addresses take no credentials.
export function buildContentRequest(ctx) {
  const state = plainObject(ctx.state);
  const data = plainObject(ctx.data);
  let url = "";
  if (ctx.artifactKey === "video") url = taskVideoURL(state, data);
  else if (ctx.artifactKey === "last_frame" && state.vendor === "seedance") url = trimmed(plainObject(data.content).last_frame_url);
  else if (ctx.artifactKey === "audio" && state.vendor === "speech") url = trimmed(plainObject(data.ttsTaskInfo).url);
  if (!url) throw new Error("artifact_not_found");
  return { url: url, method: ctx.clientRequest.method, credentialless: true };
}

// --------------------------- OpenAI video protocol --------------------------
//
// POST /v1/videos takes prompt, seconds (or duration), size, a reference image
// (image, input_reference, images, or one uploaded input_reference) and
// metadata, the vendor's own fields. Each vendor below turns such a request
// into the vendor's own body, which the checks and the usage of the native
// routes then read, so nothing is billed that is not sent.
//
// The models of SHARED_VIDEO_MODELS are read as the vendor's plugin reads
// them, step for step: its decoder's refusals here in decode, its request
// builder's in request, in its words. The other models follow the plugin
// nearest to them, and their refusals wait until the request is built: this
// gateway offers its OpenAI video plugin as one more candidate for every model
// on /v1/videos, and a request refused in decode would be left to it instead of
// answering the client.

// The model whose vendor serves a request: a channel alias stands for the
// declared model it maps to.
function videoVendor(model) {
  if (Object.prototype.hasOwnProperty.call(SEEDANCE_MODELS, model)) return "seedance";
  if (Object.prototype.hasOwnProperty.call(KLING_PATH_MODELS, model) || KLING_BODY_MODELS.includes(model)) return "kling";
  if (VIDU_MODELS.includes(model)) return "vidu";
  if (Object.prototype.hasOwnProperty.call(PIXVERSE_MODELS, model)) return "pixverse";
  if (HAPPYHORSE_MODELS.includes(model)) return "happyhorse";
  if (model === H3_MODEL || model === H3_MAX_MODEL || HAILUO_MODELS.includes(model)) return "minimax";
  return "";
}

// The fields of a form, each given once but those of listed, which repeat.
function formFields(ctx, listed) {
  const fields = ctx.body.fields || {};
  const req = {};
  for (const name of Object.keys(fields)) {
    const values = fields[name] || [];
    if ((listed || []).includes(name)) req[name] = values;
    else if (values.length > 1) throw new Error(name + " must be provided once");
    else req[name] = values[0];
  }
  return req;
}

// A form carries metadata as the text of a JSON object.
function parseFormMetadata(req) {
  if (req.metadata === undefined) return;
  let parsed;
  try {
    parsed = JSON.parse(req.metadata);
  } catch (e) {
    throw new Error("metadata must be a JSON object string");
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("metadata must be a JSON object string");
  req.metadata = parsed;
}

// The request as the kling, vidu and hailuo plugins read it: a JSON object, or
// a form with at most one uploaded file, input_reference.
function uploadableVideoRequest(ctx) {
  if (!ctx.body || (ctx.body.kind !== "json" && ctx.body.kind !== "multipart")) throw new Error("JSON or multipart body required");
  if (ctx.body.kind === "json") {
    if (!ctx.body.value || Array.isArray(ctx.body.value)) throw new Error("JSON object required");
    return { req: Object.assign({}, ctx.body.value), uploaded: false };
  }
  const req = formFields(ctx);
  let uploaded = false;
  for (const file of ctx.body.files || []) {
    if (file.field !== "input_reference") throw new Error("unexpected file field: " + file.field);
    if (uploaded) throw new Error("input_reference must be provided once");
    uploaded = true;
  }
  parseFormMetadata(req);
  if (req.seconds !== undefined) req.seconds = Number(req.seconds);
  else if (req.duration !== undefined) req.seconds = Number(req.duration);
  return { req: req, uploaded: uploaded };
}

// The request of the vendors that take reference images by address only, as
// the alibaba plugin reads it; seconds and duration name the same value.
function addressedVideoRequest(ctx) {
  if (!ctx.body || (ctx.body.kind !== "json" && ctx.body.kind !== "multipart")) throw new Error("JSON or multipart body required");
  let req;
  if (ctx.body.kind === "json") {
    if (!ctx.body.value || typeof ctx.body.value !== "object" || Array.isArray(ctx.body.value)) throw new Error("JSON object required");
    req = Object.assign({}, ctx.body.value);
  } else {
    if ((ctx.body.files || []).length) throw new Error("reference images must be URLs; files cannot be uploaded");
    req = formFields(ctx, ["images"]);
    parseFormMetadata(req);
    for (const name of ["seconds", "duration"]) {
      if (req[name] !== undefined) req[name] = Number(req[name]);
    }
  }
  const seconds = req.seconds === undefined ? req.duration : req.seconds;
  if (seconds !== undefined) req.duration = seconds;
  req.images = referenceImages([req.image, req.input_reference].concat(Array.isArray(req.images) ? req.images : []));
  return req;
}

// The reference images a request names, in order and without repeats.
function referenceImages(values) {
  const images = [];
  for (const image of values) {
    if (trimmed(image) && !images.includes(trimmed(image))) images.push(trimmed(image));
  }
  return images;
}

// An uploaded file stays a placeholder the host fills in when it sends the
// request; nothing else of a client's object reaches the upstream body.
function filePlaceholder(image) {
  if (!image || typeof image !== "object" || Array.isArray(image) || !image.__fileRef) return image;
  const placeholder = { __fileRef: image.__fileRef, encoding: image.encoding };
  if (image.mimeType) placeholder.mimeType = image.mimeType;
  if (image.maxBytes !== undefined && image.maxBytes !== null) placeholder.maxBytes = image.maxBytes;
  return placeholder;
}

// A size of the protocol, WIDTHxHEIGHT.
function sizeDimensions(size) {
  const parts = trimmed(size).toLowerCase().split("x");
  const width = Number(parts[0]);
  const height = Number(parts[1]);
  if (parts.length !== 2 || !(width > 0) || !(height > 0)) throw new Error("size must be WIDTHxHEIGHT, such as 1280x720");
  return { width: width, height: height };
}

// The aspect ratio of the list nearest the size's.
function nearestAspectRatio(dimensions, ratios) {
  let nearest = ratios[0];
  let distance = Infinity;
  for (const ratio of ratios) {
    const sides = ratio.split(":").map(Number);
    const ratioDistance = Math.abs(Math.log(dimensions.width / dimensions.height) - Math.log(sides[0] / sides[1]));
    if (ratioDistance < distance) {
      nearest = ratio;
      distance = ratioDistance;
    }
  }
  return nearest;
}

// The doubao plugin's check of the protocol's seconds: -1 lets the model pick
// the length.
function checkSeedanceSeconds(seconds) {
  if (seconds !== undefined && Number(seconds) !== -1 && (!Number.isFinite(Number(seconds)) || Number(seconds) <= 0 || Number(seconds) > 3600))
    throw new Error("seconds must be -1 or between 1 and 3600");
}

// The action of a Vidu request, which names its endpoint: metadata.action, or
// the number of reference images.
function viduProtocolAction(req) {
  if (req.metadata && req.metadata.action) {
    const aliases = { generate: "image_to_video", textGenerate: "text_to_video", firstTailGenerate: "first_tail_to_video", referenceGenerate: "reference_to_video", remixGenerate: "remix" };
    return aliases[req.metadata.action] || req.metadata.action;
  }
  if (!req.images || req.images.length === 0) return "text_to_video";
  if (req.images.length === 2) return "first_tail_to_video";
  if (req.images.length > 2) return "reference_to_video";
  return "image_to_video";
}

// The duration and resolution a request of the protocol sends Vidu: its own,
// whose size or resolution may sit in metadata and in any case, or the
// model's defaults on the endpoint. Vidu checks them for the model.
function viduProtocolSettings(req, action, model) {
  const endpoint = Object.keys(VIDU_ENDPOINTS).find((name) => VIDU_ENDPOINTS[name].action === action);
  const profile = (endpoint && viduProfile(endpoint, model)) || VIDU_WIDEST;
  const duration = req.duration === undefined || req.duration === null || req.duration === "" ? profile.durations[2] : Number(req.duration);
  if (!Number.isInteger(duration) || duration <= 0 || duration > 3600) throw new Error("seconds must be between 1 and 3600");
  const resolutions = profile.longest && duration === profile.durations[1] ? profile.longest : profile.resolutions;
  const raw = trimmed(req.resolution || (req.metadata || {}).resolution || req.size);
  let resolution = resolutions.find((item) => item.toLowerCase() === raw.toLowerCase());
  const parts = raw.toLowerCase().replace("*", "x").split("x");
  const longer = Math.max(Number(parts[0]), Number(parts[1]));
  if (!resolution && parts.length === 2 && Number(parts[0]) > 0 && Number(parts[1]) > 0) {
    const wanted = longer >= 3840 ? "4K" : longer >= 2560 ? "2K" : longer >= 1920 ? "1080p" : longer >= 1280 ? "720p" : longer >= 960 ? "540p" : "360p";
    if (resolutions.includes(wanted)) resolution = wanted;
  }
  return { duration: duration, resolution: resolution || resolutions[0] };
}

// The content of a MiniMax-H3 request is checked as the hailuo plugin checks
// it, in its words, before MiniMax reads it.
function checkH3Content(name, items) {
  const counts = { text: 0, first: 0, last: 0, frames: 0, images: 0, referenceImages: 0, videos: 0, audios: 0 };
  for (const item of items) {
    if (!item || typeof item !== "object" || Array.isArray(item)) continue;
    const role = trimmed(item.role);
    if (item.type === "text" && trimmed(item.text)) counts.text += 1;
    else if (item.type === "video_url") counts.videos += 1;
    else if (item.type === "audio_url") counts.audios += 1;
    if (item.type !== "image_url") continue;
    counts.images += 1;
    if (!role || role === "first_frame") counts.first += 1;
    else if (role === "last_frame") counts.last += 1;
    if (!role || role === "first_frame" || role === "last_frame" || role === "middle_frame") counts.frames += 1;
    else if (role === "reference_image") counts.referenceImages += 1;
  }
  if (!counts.text) throw new Error(name + " requires a non-empty text item");
  if (counts.first > 1) throw new Error(name + " accepts at most one first_frame image");
  if (counts.last > 1) throw new Error(name + " accepts at most one last_frame image");
  if (counts.referenceImages > H3_MAX_REFERENCE_IMAGES) throw new Error(name + " accepts at most " + H3_MAX_REFERENCE_IMAGES + " reference images");
  if (counts.images > H3_MAX_REFERENCE_IMAGES) throw new Error(name + " accepts at most " + H3_MAX_REFERENCE_IMAGES + " input images");
  if (counts.videos > H3_MAX_REFERENCE_VIDEOS) throw new Error(name + " accepts at most " + H3_MAX_REFERENCE_VIDEOS + " reference videos");
  if (counts.audios > H3_MAX_REFERENCE_AUDIOS) throw new Error(name + " accepts at most " + H3_MAX_REFERENCE_AUDIOS + " reference audios");
  if (counts.frames && (counts.referenceImages || counts.videos || counts.audios)) throw new Error(name + " cannot mix frame images with reference media");
  return items;
}

// One value or a list of them; an uploaded file stays a placeholder.
function h3MediaList(source, key) {
  const raw = source[key];
  if (raw === undefined || raw === null) return [];
  return (Array.isArray(raw) ? raw : [raw]).filter((value) => (value && typeof value === "object" ? true : Boolean(trimmed(value))));
}

function h3MediaItem(type, url, role) {
  const item = { type: type, role: role };
  item[type] = { url: url };
  return item;
}

// The content array of a MiniMax-H3 request: metadata.content as it is, with
// the prompt when it has no text, or the prompt, the frame images and the
// reference media of metadata.
function h3ProtocolContent(name, req) {
  const metadata = req.metadata || {};
  const prompt = trimmed(req.prompt);
  if (metadata.content !== undefined && metadata.content !== null) {
    if (!Array.isArray(metadata.content)) throw new Error("metadata.content must be an array");
    if (metadata.content.some((item) => item && item.type === "text" && trimmed(item.text))) return checkH3Content(name, metadata.content);
    if (!prompt) throw new Error(name + " metadata.content requires a text item or a prompt");
    return checkH3Content(name, [{ type: "text", text: prompt }].concat(metadata.content));
  }
  const content = prompt ? [{ type: "text", text: prompt }] : [];
  const images = h3MediaList(req, "images");
  if (images.length > H3_MAX_FRAME_IMAGES) throw new Error(name + " accepts at most " + H3_MAX_FRAME_IMAGES + " frame images");
  if (metadata.first_frame_image) content.push(h3MediaItem("image_url", metadata.first_frame_image, "first_frame"));
  if (metadata.last_frame_image) content.push(h3MediaItem("image_url", metadata.last_frame_image, "last_frame"));
  if (!metadata.first_frame_image && !metadata.last_frame_image) {
    images.forEach((url, index) => content.push(h3MediaItem("image_url", url, index === 0 ? "first_frame" : "last_frame")));
  }
  const videos = h3MediaList(metadata, "reference_video");
  if (videos.length > H3_MAX_REFERENCE_VIDEOS) throw new Error(name + " accepts at most " + H3_MAX_REFERENCE_VIDEOS + " reference videos");
  for (const video of videos) content.push(h3MediaItem("video_url", video, "reference_video"));
  const audios = h3MediaList(metadata, "reference_audio");
  if (audios.length > H3_MAX_REFERENCE_AUDIOS) throw new Error(name + " accepts at most " + H3_MAX_REFERENCE_AUDIOS + " reference audios");
  for (const audio of audios) content.push(h3MediaItem("audio_url", audio, "reference_audio"));
  if (!content.length) throw new Error(name + " requires a prompt or a media input");
  return checkH3Content(name, content);
}

// Each vendor's reading of the protocol: decode normalizes the client's
// request and names the action, request builds the vendor request from it for
// the model the channel sends.
const VIDEO_PROTOCOL = {
  // Seedance, as the doubao plugin: seconds reaches Ark as duration, the
  // images lead the content before those of metadata.content, and the rest
  // of metadata is Ark's own fields.
  seedance: {
    decode: function (ctx) {
      if (!ctx.body || (ctx.body.kind !== "json" && ctx.body.kind !== "multipart")) throw new Error("JSON or multipart body required");
      if (ctx.body.kind === "json") {
        if (!ctx.body.value || Array.isArray(ctx.body.value)) throw new Error("JSON object required");
        const body = ctx.body.value;
        const seconds = body.seconds === undefined ? body.duration : body.seconds;
        checkSeedanceSeconds(seconds);
        const req = Object.assign({}, body);
        if (seconds !== undefined) req.seconds = seconds;
        const images = referenceImages([body.image, body.input_reference].concat(Array.isArray(body.images) ? body.images : []));
        if (images.length) req.images = images;
        return { request: req, action: images.length ? "image_to_video" : "text_to_video" };
      }
      const req = formFields(ctx);
      parseFormMetadata(req);
      // One uploaded input_reference is the first frame, which Ark reads as a
      // data URL of a lowercase image type; every other reference is a URL
      // inside metadata.content.
      const images = referenceImages([req.image, req.input_reference]);
      for (const file of ctx.body.files || []) {
        if (file.field !== "input_reference") throw new Error("only input_reference may be uploaded; other references must be URLs inside metadata.content");
        if (images.length) throw new Error("input_reference must be provided once");
        const mimeType = trimmed(file.mimeType).toLowerCase();
        images.push({ __fileRef: file.ref, encoding: "dataUrl", mimeType: /^image\/[a-z0-9.+-]+$/.test(mimeType) ? mimeType : "image/png", maxBytes: 31457280 });
      }
      if (images.length) req.images = images;
      if (req.seconds !== undefined) req.seconds = Number(req.seconds);
      else if (req.duration !== undefined) req.seconds = Number(req.duration);
      checkSeedanceSeconds(req.seconds);
      return { request: req, action: images.length ? "image_to_video" : "text_to_video" };
    },
    request: function (ctx, decoded) {
      const req = decoded.request;
      const metadata = req.metadata || {};
      const body = Object.assign({ content: [] }, metadata);
      const images = (Array.isArray(req.images) ? req.images : []).map((url) => ({ type: "image_url", image_url: { url: url } }));
      const content = images.concat(Array.isArray(body.content) ? body.content : []).filter((item) => item && item.type !== "text");
      const hasReference = content.length > 0;
      if (trimmed(req.prompt) || !hasReference) content.push({ type: "text", text: req.prompt || "" });
      // A draft names an earlier task, which this protocol does not resolve.
      if (content.some((item) => item.type === "draft_task" && trimmed(plainObject(item.draft_task).id))) throw new Error("origin task is unavailable");
      body.content = content;
      // -1 lets the model pick the length; the longest is then reserved.
      const seconds = Number.parseInt(req.seconds || "", 10);
      if (seconds > 0 || seconds === -1) body.duration = seconds;
      // The tier of a WIDTHxHEIGHT size, which Ark is not sent, is the
      // resolution reserved; the one Ark reports settles the task.
      const stated = trimmed(metadata.resolution || req.size).toLowerCase();
      const sides = stated.replace("*", "x").split("x");
      const longer = Math.max(Number(sides[0]), Number(sides[1]));
      let resolution = SEEDANCE_RESOLUTIONS.includes(stated) ? stated : "";
      if (!resolution && sides.length === 2) resolution = longer >= 3840 ? "4k" : longer >= 1920 ? "1080p" : longer >= 1280 ? "720p" : "480p";
      return {
        vendor: "seedance",
        model: decoded.model,
        vendorBody: body,
        action: hasReference ? "image_to_video" : "text_to_video",
        reserved: { duration: req.seconds || req.duration || metadata.duration, frames: metadata.frames, resolution: resolution, content: metadata.content },
      };
    },
  },

  // Kling, as the kling plugin: the prompt, the first frame, the mode and the
  // aspect ratio nearest the size, with Kling's other fields in metadata. The
  // API that takes the model in its path gets a resolution from the size and
  // a last frame from metadata.image_tail; the other always gets a mode and a
  // duration, Kling's defaults when the request names none.
  kling: {
    decode: function (ctx) {
      const read = uploadableVideoRequest(ctx);
      const req = read.req;
      const seconds = req.seconds === undefined ? req.duration : req.seconds;
      if (seconds !== undefined) req.duration = klingDuration(seconds, 1, 3600, "duration must be a positive integer of at most 3600");
      if (read.uploaded) req.image = { __fileRef: "request_file:input_reference", encoding: "base64", maxBytes: 10485760 };
      else if (trimmed(req.input_reference || req.image)) req.image = trimmed(req.input_reference || req.image);
      const metadata = req.metadata || {};
      req.mode = trimmed(req.mode || metadata.mode).toLowerCase() || "std";
      const hasImage = read.uploaded || Boolean(trimmed(req.input_reference) || trimmed(req.image) || metadata.image || metadata.image_tail);
      return { request: req, action: hasImage ? "image_to_video" : "text_to_video" };
    },
    request: function (ctx, decoded) {
      const req = decoded.request;
      const model = decoded.model;
      if (Object.prototype.hasOwnProperty.call(KLING_PATH_MODELS, model)) {
        const metadata = Object.assign({}, plainObject(req.metadata));
        const frames = [
          ["first_frame", req.image || metadata.image],
          ["last_frame", metadata.image_tail],
        ].filter((frame) => frame[1]);
        let duration = metadata.duration;
        if (duration === undefined || duration === null) duration = req.duration;
        for (const name of ["mode", "duration", "image", "image_tail"]) delete metadata[name];
        const endpoint = frames.length ? "image-to-video" : "text-to-video";
        const settings = {};
        if (trimmed(req.size)) {
          const dimensions = sizeDimensions(req.size);
          // The resolution whose shorter side is nearer the size's.
          settings.resolution = Math.min(dimensions.width, dimensions.height) < 900 ? "720p" : "1080p";
          if (endpoint === "text-to-video") settings.aspect_ratio = nearestAspectRatio(dimensions, KLING_PATH_ASPECT_RATIOS);
        }
        // Without a duration Kling picks the length.
        if (duration !== undefined && duration !== null) settings.duration = klingDuration(duration, KLING_PATH_MIN_SECONDS, KLING_PATH_MODELS[model].maxSeconds);
        let body = req.prompt ? { prompt: req.prompt } : {};
        if (frames.length) body = { contents: (req.prompt ? [{ type: "prompt", text: req.prompt }] : []).concat(frames.map((frame) => ({ type: frame[0], url: filePlaceholder(frame[1]) }))) };
        if (Object.keys(settings).length) body.settings = settings;
        Object.assign(body, metadata);
        checkKlingPathBody(model, body);
        return { vendor: "kling", api: "path", model: model, endpoint: endpoint, vendorBody: body, action: KLING_PATH_ACTIONS[endpoint] };
      }
      const metadata = req.metadata || {};
      const hasImage = Boolean(req.image || metadata.image || metadata.image_tail);
      let action = ctx.action === "text_to_video" || ctx.action === "image_to_video" ? ctx.action : hasImage ? "image_to_video" : "text_to_video";
      const omni = KLING_OMNI_MODELS.includes(model);
      if (omni) action = hasImage ? "image_to_video" : "text_to_video";
      const body = {};
      if (req.prompt) body.prompt = req.prompt;
      if (req.image) body.image = req.image;
      if (req.mode) body.mode = req.mode;
      // Image-to-video has no aspect ratio.
      if (action === "text_to_video" && req.size !== undefined && req.size !== null && req.size !== "") body.aspect_ratio = nearestAspectRatio(sizeDimensions(req.size), KLING_BODY_ASPECT_RATIOS);
      Object.assign(body, metadata);
      body.mode = trimmed(body.mode).toLowerCase() || "std";
      let duration = metadata.duration;
      if (duration === undefined || duration === null) duration = req.duration;
      body.duration = String(duration === undefined || duration === null ? 5 : klingDuration(duration, 1, 3600, "duration must be a positive integer of at most 3600"));
      if (body.image) body.image = filePlaceholder(body.image);
      if (body.image_tail) body.image_tail = filePlaceholder(body.image_tail);
      if (!body.prompt) delete body.prompt;
      if (!body.image) delete body.image;
      let endpoint = action === "image_to_video" ? "image2video" : "text2video";
      if (omni) {
        // The omni-video endpoint takes its frames as a list.
        endpoint = "omni-video";
        const frames = [
          ["first_frame", body.image],
          ["end_frame", body.image_tail],
        ].filter((frame) => frame[1]);
        delete body.image;
        delete body.image_tail;
        if (frames.length && body.image_list === undefined) body.image_list = frames.map((frame) => ({ image_url: frame[1], type: frame[0] }));
      }
      return { vendor: "kling", api: "body", model: model, endpoint: endpoint, vendorBody: body, action: KLING_BODY_ACTIONS[endpoint] };
    },
  },

  // Vidu, as the vidu plugin: the prompt and the images in Vidu's fields and
  // Vidu's other fields in metadata, which cannot replace the billed model,
  // duration and resolution.
  vidu: {
    decode: function (ctx, model) {
      const read = uploadableVideoRequest(ctx);
      const req = read.req;
      const seconds = req.seconds === undefined ? req.duration : req.seconds;
      if (seconds !== undefined) req.duration = Number(seconds);
      if (read.uploaded) req.images = [{ __fileRef: "request_file:input_reference", encoding: "dataUrl", maxBytes: 15728640 }];
      else if (trimmed(req.input_reference || req.image) && (!Array.isArray(req.images) || req.images.length === 0)) req.images = [trimmed(req.input_reference || req.image)];
      const action = viduProtocolAction(req);
      const settings = viduProtocolSettings(req, action, model);
      req.duration = settings.duration;
      req.resolution = settings.resolution;
      return { request: req, action: action };
    },
    request: function (ctx, decoded) {
      const req = decoded.request;
      const action = viduProtocolAction(req);
      const body = Object.assign({}, req.metadata || {}, viduProtocolSettings(req, action, upstreamModel(ctx)));
      if (trimmed(req.prompt)) body.prompt = req.prompt;
      if (Array.isArray(req.images) && req.images.length) body.images = req.images;
      delete body.action;
      if (Array.isArray(body.images)) body.images = body.images.map(filePlaceholder);
      const endpoint = Object.keys(VIDU_ENDPOINTS).find((name) => VIDU_ENDPOINTS[name].action === action) || "text2video";
      return { vendor: "vidu", model: decoded.model, endpoint: endpoint, vendorBody: body, action: action };
    },
  },

  // MiniMax-H3, as the hailuo plugin: a content array of the prompt, the frame
  // images and metadata's reference media, or metadata.content itself, with a
  // duration of 5 seconds, 768P and, for text alone, 16:9 when the request
  // names none. MiniMax-H3-MAX follows it with its own resolutions. The /v1
  // models, as the hailuo plugin too: the prompt, a duration of 6 seconds and
  // the model's default resolution when the request names none, and MiniMax's
  // frame fields from metadata.
  minimax: {
    decode: function (ctx, model) {
      const read = uploadableVideoRequest(ctx);
      const req = read.req;
      const seconds = req.seconds === undefined ? req.duration : req.seconds;
      if (seconds !== undefined) req.duration = Number(seconds);
      // The reference image is the first frame unless metadata names one.
      const image = trimmed(req.input_reference || req.image);
      if (read.uploaded) req.metadata = Object.assign({}, req.metadata || {}, { first_frame_image: { __fileRef: "request_file:input_reference", encoding: "dataUrl", maxBytes: 20971520 } });
      else if (image) {
        req.metadata = Object.assign({}, req.metadata || {});
        if (!req.metadata.first_frame_image) req.metadata.first_frame_image = image;
      }
      const metadata = req.metadata || {};
      const named = trimmed(req.input_reference) || trimmed(req.image) || (Array.isArray(req.images) && req.images.length);
      const hasImage = read.uploaded || Boolean(named || hailuoHasImage(metadata));
      if (HAILUO_MODELS.includes(model)) checkHailuoRequest(model, req.duration === undefined ? undefined : Number(req.duration), hailuoProtocolResolution(req, model), hasImage);
      return { request: req, action: hasImage ? "image_to_video" : "text_to_video" };
    },
    request: function (ctx, decoded) {
      const req = decoded.request;
      const metadata = req.metadata || {};
      if (HAILUO_MODELS.includes(decoded.model)) {
        const seconds = Number(req.duration);
        const facts = { seconds: Number.isFinite(seconds) && seconds > 0 ? seconds : 6, resolution: hailuoProtocolResolution(req, upstreamModel(ctx)), input_images: 0, input_video_seconds: 0 };
        const body = { duration: facts.seconds, resolution: facts.resolution };
        if (req.prompt) body.prompt = req.prompt;
        for (const name of HAILUO_METADATA_FIELDS) {
          if (metadata[name] !== undefined && metadata[name] !== null) body[name] = metadata[name];
        }
        const named = trimmed(req.input_reference) || trimmed(req.image) || (Array.isArray(req.images) && req.images.length);
        return { vendor: "minimax", api: "v1", model: decoded.model, vendorBody: body, action: named || hailuoHasImage(metadata) ? "image_to_video" : "text_to_video", facts: facts };
      }
      const name = upstreamModel(ctx) === H3_MODEL ? H3_MODEL : H3_MAX_MODEL;
      const content = h3ProtocolContent(name, req);
      const duration = req.duration === undefined || req.duration === null || req.duration === "" ? H3_DEFAULT_DURATION : h3Duration(name, req.duration);
      // A resolution or a size names the tier it contains.
      const stated = (trimmed(metadata.resolution) || trimmed(req.resolution) || trimmed(req.size)).toUpperCase();
      const tiers = name === H3_MODEL ? ["2K", "768"] : ["480", "768"];
      const tier = stated ? tiers.find((item) => stated.includes(item)) : "768";
      if (!tier) throw new Error(name + " resolution must be " + (name === H3_MODEL ? "768P or 2K" : "480P or 768P"));
      const resolution = tier === "2K" ? tier : tier + "P";
      // Text alone needs a ratio other than adaptive.
      const media = content.some((item) => item && (item.type === "image_url" || item.type === "video_url" || item.type === "audio_url"));
      const body = { content: content, resolution: resolution, duration: duration, ratio: trimmed(metadata.ratio) || (media ? "adaptive" : "16:9") };
      if (metadata.aigc_watermark !== undefined && metadata.aigc_watermark !== null) body.aigc_watermark = metadata.aigc_watermark;
      const visual = content.some((item) => item && (item.type === "image_url" || item.type === "video_url"));
      return { vendor: "minimax", model: decoded.model, vendorBody: body, action: visual ? "image_to_video" : "text_to_video" };
    },
  },

  // PixVerse, after the vidu plugin: the prompt, the reference images (one for
  // image-to-video, two for the first and the last frame) and PixVerse's own
  // fields in metadata, whose action names another endpoint. The duration and
  // the quality, which the size names by its shorter side, are sent as they
  // are billed, 5 seconds and 720p when the request names none.
  pixverse: {
    decode: function (ctx) {
      const req = addressedVideoRequest(ctx);
      const action = trimmed(plainObject(req.metadata).action) || (req.images.length > 1 ? "first_tail_to_video" : req.images.length ? "image_to_video" : "text_to_video");
      return { request: req, action: action };
    },
    request: function (ctx, decoded) {
      const req = decoded.request;
      const metadata = plainObject(req.metadata);
      const action = trimmed(metadata.action) || (req.images.length > 1 ? "first_tail_to_video" : req.images.length ? "image_to_video" : "text_to_video");
      const endpoint = Object.keys(PIXVERSE_ENDPOINTS).find((name) => PIXVERSE_ENDPOINTS[name] === action);
      if (!endpoint) throw new Error("metadata.action must be one of " + Object.keys(PIXVERSE_ENDPOINTS).map((name) => PIXVERSE_ENDPOINTS[name]).join(", "));
      const body = {};
      if (trimmed(req.prompt)) body.prompt = req.prompt;
      if (req.images.length) body.image_list = req.images.map((url) => ({ image_url: url }));
      const size = trimmed(req.size).toLowerCase();
      let quality = PIXVERSE_QUALITIES.includes(size) ? size : "";
      if (size && !quality) {
        const dimensions = sizeDimensions(size);
        const shorter = Math.min(dimensions.width, dimensions.height);
        quality = shorter >= 1080 ? "1080p" : shorter >= 720 ? "720p" : shorter >= 540 ? "540p" : "360p";
        if (endpoint === "text") body.aspect_ratio = nearestAspectRatio(dimensions, PIXVERSE_ASPECT_RATIOS);
      }
      Object.assign(body, metadata);
      delete body.action;
      body.duration = req.duration ?? metadata.duration ?? PIXVERSE_DEFAULT_SECONDS;
      body.quality = metadata.quality ?? req.resolution ?? (quality || PIXVERSE_DEFAULT_QUALITY);
      return { vendor: "pixverse", model: decoded.model, endpoint: endpoint, vendorBody: body, action: action };
    },
  },

  // HappyHorse, after the alibaba plugin: the prompt and the reference images
  // in input, the duration, the resolution and, for text-to-video, the ratio
  // of the size in parameters, and DashScope's own input and parameters in
  // metadata. A duration or resolution the request leaves out is left to
  // Bailian: the longest and the dearest are reserved and its report settles.
  happyhorse: {
    decode: function (ctx, model) {
      return { request: addressedVideoRequest(ctx), action: model.endsWith("-t2v") ? "text_to_video" : "image_to_video" };
    },
    request: function (ctx, decoded) {
      const req = decoded.request;
      const model = decoded.model;
      const metadata = plainObject(req.metadata);
      for (const name of ["input", "parameters"]) {
        const value = metadata[name];
        if (value !== undefined && value !== null && (typeof value !== "object" || Array.isArray(value))) throw new Error("metadata." + name + " must be an object");
      }
      const input = {};
      if (trimmed(req.prompt)) input.prompt = req.prompt;
      // Image-to-video takes its frames, the other models reference images.
      if (req.images.length) input.media = req.images.map((url, index) => ({ type: !model.endsWith("-i2v") ? "reference_image" : index === 0 ? "first_frame" : "last_frame", url: url }));
      Object.assign(input, plainObject(metadata.input));
      const parameters = {};
      const size = trimmed(req.size);
      if (trimmed(req.resolution)) parameters.resolution = trimmed(req.resolution).toUpperCase().replace(/^(\d+)$/, "$1P");
      else if (size) {
        const dimensions = sizeDimensions(size);
        parameters.resolution = Math.min(dimensions.width, dimensions.height) >= 1080 ? "1080P" : "720P";
        if (model.endsWith("-t2v")) parameters.ratio = nearestAspectRatio(dimensions, HAPPYHORSE_ASPECT_RATIOS);
      }
      if (req.duration !== undefined && req.duration !== null) parameters.duration = req.duration;
      Object.assign(parameters, plainObject(metadata.parameters));
      return { vendor: "happyhorse", model: model, vendorBody: { input: input, parameters: parameters }, action: model.endsWith("-t2v") ? "text_to_video" : "image_to_video" };
    },
  },
};

export const protocols = {
  openai_video: {
    decodeRequest: function (ctx) {
      const model = [ctx.upstreamModel, ctx.model].map(trimmed).find(videoVendor);
      if (!model) throw new Error("model " + trimmed(ctx.model) + " is not served on /v1/videos");
      const requestBody = { vendor: videoVendor(model), model: model, protocol: "openai_video" };
      const intent = { kind: "submit", model: ctx.model, requestBody: requestBody };
      try {
        const decoded = VIDEO_PROTOCOL[requestBody.vendor].decode(ctx, model);
        // vendorBody carries a native request's own body; a client of this
        // protocol cannot set it.
        delete decoded.request.vendorBody;
        requestBody.request = decoded.request;
        intent.action = decoded.action;
      } catch (error) {
        if (SHARED_VIDEO_MODELS.includes(model)) throw error;
        requestBody.invalid = String((error && error.message) || error);
      }
      return intent;
    },
    // The host sets id, object, model, status, progress and the timestamps.
    // What a vendor's plugin adds to them from the vendor's last reply is
    // added here for the same reply: the output's length for Kling and each
    // vendor's own error.
    render: function (ctx, task) {
      const data = plainObject(task.data);
      const output = {};
      if (data.data && typeof data.data === "object") {
        // Kling: the task of the API that takes the model in its path is data
        // itself or the only entry of a list, the other API's has a task_id.
        const path = Array.isArray(data.data) || Boolean(data.data.id && !data.data.task_id);
        const entry = path ? klingPathEntry(data) : data.data;
        const videos = path ? klingPathVideos(entry) : plainObject(entry.task_result).videos || [];
        if (videos.length && videos[0].duration) output.seconds = videos[0].duration;
        if (data.code !== 0 && data.message) output.error = { message: data.message, code: String(data.code) };
        if (path && entry.status === "failed") output.error = { message: entry.message || "", code: "" };
        if (!path && entry.task_status === "failed") output.error = { message: entry.task_status_msg, code: "" };
        return output;
      }
      const error = plainObject(data.error);
      const result = plainObject(data.output);
      if (data.status === "failed") output.error = { message: error.message || "", code: error.code || "" };
      else if (data.state === "failed" && data.err_code) output.error = { message: data.err_code, code: data.err_code };
      else if (data.base_resp && data.base_resp.status_code !== 0) output.error = { message: data.base_resp.status_msg, code: String(data.base_resp.status_code) };
      else if (data.output && data.code) output.error = { code: data.code, message: data.message || "" };
      else if (result.code) output.error = { code: result.code, message: result.message || "" };
      else if (data.taskId && trimmed(data.status).toUpperCase() === "FAILED") output.error = { message: taskDetailResult(data).reason, code: "" };
      return output;
    },
  },
};

// A key is the API key alone, or the API key with an AccessKey and its
// SecretKey.
export function checkKey(_ctx, key) {
  const parts = String(key || "").split("|").map(trimmed);
  if ((parts.length === 1 || parts.length === 3) && parts.every(Boolean)) return;
  return { error: { en: "The key must be an API Key, or API Key|AccessKey|SecretKey", zh: "密钥应为 API Key，或 API Key|AccessKey|SecretKey" } };
}
