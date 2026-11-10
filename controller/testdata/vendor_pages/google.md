> ## AI Agent Context
> Get the complete documentation index at: https://aistudio.google.com/llms.txt
> Use this file to discover all available pages.
> For the full documentation in a single file, fetch: https://aistudio.google.com/llms-full.txt

# Gemini Developer API pricing

Start building free of charge with generous limits, then scale up with prepaid
and pay-as-you-go pricing for your production-ready applications.

## Gemini 4 Argon  {% id="gemini-4-argon" %}

{% two-column-layout %}
  {% left-column class="center-vertically" %}
  *[`gemini-4-argon`](/docs/models/gemini-4-argon)*

  {% /left-column %}

  {% right-column class="center-vertically right-align" %}
  {% button href="https://aistudio.google.com/prompts/new_chat?model=gemini-4-argon" variant="ais" %}
  Try it in Google AI Studio

  {% /button %}

  {% /right-column %}

{% /two-column-layout %}

{% hr /%}

Our flagship reasoning model, built for complex software engineering, deep
mathematical and scientific problem solving, autonomous multi-step agents, and
long-decode generation up to 1M tokens.

{% callout type="note" %}
  Gemini 4 Argon is not available on the Free tier. Every token the model processes is counted and billed, including thinking tokens and internal tool system instructions and function declarations when using grounding.

{% /callout %}

{% tabs %}
  {% tab title="Standard" %}
  {% table %}
  * Feature
  * Free Tier
  * Paid Tier, per 1M tokens in USD

  ---

  * Input price
  * Not available
  * $2.00 through November 30, 2026.   
  $4.00 starting December 1, 2026.

  ---

  * Output price (including thinking tokens)
  * Not available
  * $10.00 through November 30, 2026.   
  $20.00 starting December 1, 2026.

  ---

  * Context caching price
  * Not available
  * $0.10 through November 30, 2026.   
  $0.20 starting December 1, 2026.   
  Implicit caching only (explicit caching is not supported).

  ---

  * Grounding with Google Search{% sup %}*{% /sup %}
  * Not available
  * 5,000 free search requests per month (shared across all Gemini 3 and newer models),
  then $14 per 1,000 requests (subject to a [daily cap](/docs/pricing#pricing-for-tools) of 1,500 requests per day).

  ---

  * Grounding with Google Maps
  * Not available
  * 5,000 requests per month (free, shared across all Gemini 3 and newer models), then $14 / 1,000
  search queries.

  ---

  * Used to improve our products
  * Not available
  * [No](/docs/terms)
  {% /table %}

## Gemini 3.5 Transcribe  {% id="gemini-3.5-transcribe" %}

{% two-column-layout %}
  {% left-column %}
  *[`gemini-3.5-transcribe`](/docs/models/gemini-3.5-transcribe)*

  {% /left-column %}

  {% right-column class="center-vertically right-align" %}
  {% button href="https://aistudio.google.com?model=gemini-3.5-transcribe" variant="ais" %}
  Try it in Google AI Studio

  {% /button %}

  {% /right-column %}

{% /two-column-layout %}

{% hr /%}

Our speech-to-text model with automatic language detection, speaker diarization,
word-level timestamps, and custom vocabulary biasing.

{% tabs %}
  {% tab title="Standard" %}
  {% table %}
  * Feature
  * Free Tier
  * Paid Tier, per 1M tokens in USD

  ---

  * Input price
  * Free of charge
  * $2.00 or $0.003/min{% sup %}*{% /sup %} (audio)

  ---

  * Output price (including thinking tokens)
  * Free of charge
  * $12.00 or $0.002/min{% sup %}*{% /sup %} (text)

  ---

  * Grounding with Google Search
  * Not Supported
  * 

  ---

  * Used to improve our products
  * [Yes](/docs/terms)
  * [No](/docs/terms)
  {% /table %}

## Gemini 3.1 Flash Image (Nano Banana 2) 🍌  {% id="gemini-3.1-flash-image" %}

{% two-column-layout %}
  {% left-column class="center-vertically" %}
  *[`gemini-3.1-flash-image`](/docs/models/gemini-3.1-flash-image)*

  {% /left-column %}

  {% right-column class="center-vertically right-align" %}
  {% button href="https://aistudio.google.com/prompts/new_chat?model=gemini-3.1-flash-image" variant="ais" %}
  Try it in Google AI Studio

  {% /button %}

  {% /right-column %}

{% /two-column-layout %}

{% hr /%}

Designed for speed and efficiency, the Gemini 3.1 Flash Image generation model
is effective for quick, interactive responses and high throughput.

{% tabs %}
  {% tab title="Standard" %}
  {% table %}
  * Feature
  * Free Tier
  * Paid Tier, per 1M tokens in USD

  ---

  * Input price
  * Not available
  * $0.50 (text/image)

  ---

  * Output price
  * Not available
  * $3 (text and thinking)    
  $60.00 (images)    
  Equivalent to $0.045 per 0.5K image{% sup /%}    
  $0.067 per 1K image{% sup /%},    
  $0.101 per 2K image{% sup /%},    
  and $0.151 per 4K image{% sup /%}.

  ---

  * Grounding with Google Web and Image Search{% sup %}**{% /sup %}
  * Not available{% sup %}**{% /sup %}
  * 5,000 free search requests per month (shared across all Gemini 3 and newer models),
  then $14 per 1,000 requests for text and image-based grounding (subject to a [daily cap](/docs/pricing#pricing-for-tools) of 1,500 requests per day).

  ---

  * Used to improve our products
  * [Yes](/docs/terms)
  * [No](/docs/terms)
  {% /table %}

## Gemini 3.1 Flash TTS Preview  {% id="gemini-3.1-flash-tts-preview" %}

{% two-column-layout %}
  {% left-column class="center-vertically" %}
  *[`gemini-3.1-flash-tts-preview`](/docs/models/gemini-3.1-flash-tts-preview)*

  {% /left-column %}

  {% right-column class="center-vertically right-align" %}
  {% button href="https://aistudio.google.com/prompts/new_chat?model=gemini-3.1-flash-tts-preview" variant="ais" %}
  Try it in Google AI Studio

  {% /button %}

  {% /right-column %}

{% /two-column-layout %}

{% hr /%}

Our 3.1 Flash Text-to-Speech audio model optimized for price-performant,
low-latency, controllable speech generation.

{% tabs %}
  {% tab title="Standard" %}
  {% table %}
  * Feature
  * Free Tier
  * Paid Tier, per 1M tokens in USD

  ---

  * Input price
  * Free of charge
  * $1.00 (text)

  ---

  * Output price
  * Free of charge
  * $20.00 (audio)

  ---

  * Used to improve our products
  * [Yes](/docs/terms)
  * [No](/docs/terms)
  {% /table %}

## Gemini 2.5 Pro  {% id="gemini-2.5-pro" %}

*[`gemini-2.5-pro`](/docs/models/gemini-2.5-pro)*

{% hr /%}

{% callout type="caution" %}
  Gemini 2.5 Pro (`gemini-2.5-pro`) is
  [deprecated](/docs/deprecations#gemini-2.5-pro-models) and is no longer
  available to select in Google AI Studio. Effective December 1, 2026, all API
  requests to `gemini-2.5-pro` will automatically route to
  [Gemini 3.8 Flash (`gemini-3.8-flash`)](/docs/models/gemini-3.8-flash) and be
  billed at [Gemini 3.8 Flash rates](/docs/pricing#gemini-3.8-flash).

{% /callout %}

A Pro model which excels at coding and complex reasoning tasks.

{% tabs %}
  {% tab title="Standard" %}
  {% table %}
  * Feature
  * Free Tier
  * Paid Tier, per 1M tokens in USD

  ---

  * Input price
  * Free of charge
  * $1.25, prompts <= 200k tokens    
  $2.50, prompts > 200k tokens

  ---

  * Output price (including thinking tokens)
  * Free of charge
  * $10.00, prompts <= 200k tokens    
  $15.00, prompts > 200k

  ---

  * Context caching price
  * Not available
  * $0.125, prompts <= 200k tokens    
  $0.25, prompts > 200k    
  $4.50 / 1,000,000 tokens per hour (storage price)

  ---

  * Grounding with Google Search
  * Not available
  * 1,500 RPD (free), then $35 / 1,000 grounded prompts

  ---

  * Grounding with Google Maps
  * Not available
  * 10,000 RPD (free), then $25 / 1,000 grounded prompts

  ---

  * Used to improve our products
  * [Yes](/docs/terms)
  * [No](/docs/terms)
  {% /table %}

## Gemini 2.5 Flash  {% id="gemini-2.5-flash" %}

*[`gemini-2.5-flash`](/docs/models/gemini-2.5-flash)*

{% hr /%}

{% callout type="caution" %}
  Gemini 2.5 Flash (`gemini-2.5-flash`) is
  [deprecated](/docs/deprecations#gemini-2.5-flash-models) and is no longer
  available to select in Google AI Studio. Effective December 1, 2026, all API
  requests to `gemini-2.5-flash` will automatically route to
  [Gemini 3.5 Flash-Lite (`gemini-3.5-flash-lite`)](/docs/models/gemini-3.5-flash-lite)
  and be billed at
  [Gemini 3.5 Flash-Lite rates](/docs/pricing#gemini-3.5-flash-lite).

{% /callout %}

Our first hybrid reasoning model which supports a 1M token context window and
has thinking budgets.

{% tabs %}
  {% tab title="Standard" %}
  {% table %}
  * Feature
  * Free Tier
  * Paid Tier, per 1M tokens in USD

  ---

  * Input price
  * Free of charge
  * $0.30 (text / image / video)    
  $1.00 (audio)

  ---

  * Output price (including thinking tokens)
  * Free of charge
  * $2.50

  ---

  * Context caching price
  * Not available
  * $0.03 (text / image / video)    
  $0.1 (audio)    
  $1.00 / 1,000,000 tokens per hour (storage price)

  ---

  * Grounding with Google Search
  * Free of charge, up to 500 RPD (limit shared with Flash-Lite RPD)
  * 1,500 RPD (free, limit shared with Flash-Lite RPD), then $35 / 1,000
  grounded prompts

  ---

  * Grounding with Google Maps
  * 500 RPD
  * 1,500 RPD (free), then $25 / 1,000 grounded prompts

  ---

  * Used to improve our products
  * [Yes](/docs/terms)
  * [No](/docs/terms)
  {% /table %}

## Gemini Embedding  {% id="gemini-embedding" %}

{% two-column-layout %}
  {% left-column class="center-vertically" %}
  *[`gemini-embedding-001`](/docs/models/gemini-embedding-001)*

  {% /left-column %}

  {% right-column class="center-vertically right-align" %}
  {% button href="/docs/embeddings" variant="ais" %} Try the
      API
      {% /button %}

  {% /right-column %}

{% /two-column-layout %}

{% hr /%}

Our first Gemini Embeddings model for text-only use cases.

{% tabs %}
  {% tab title="Standard" %}
  {% table %}
  * Feature
  * Free Tier
  * Paid Tier, per 1M tokens in USD

  ---

  * Input price
  * Free of charge
  * $0.15

  ---

  * Used to improve our products
  * [Yes](/docs/terms)
  * [No](/docs/terms)
  {% /table %}

