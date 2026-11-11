package controller

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// ByteDance publishes its list prices twice: BytePlus ModelArk for the
// international site, in USD under dated model IDs (seed-2-0-pro-260328), and
// Volcengine Ark for mainland China, in CNY under model family names
// (doubao-seed-2.1-pro). Both pages hold a standard online inference table
// with one row per input-length tier. Models of other vendors hosted there are
// priced by their own vendor and left out.
const (
	customVendorBytePlusProvider   = "BytePlus ModelArk pricing page"
	customVendorVolcengineProvider = "Volcengine Ark pricing page"
	// How many CNY one USD buys when a vendor lists its prices in CNY. It is a
	// setting of its own: the gateway's payment exchange rate is 1 where
	// payments are taken in USD.
	customVendorCNYPerUSDOption = "VendorPriceCNYPerUSD"
)

var (
	// "[0, 32]" and "(32, 128]": the upper bound of an input-length tier, in thousand tokens.
	customArkTierBound = regexp.MustCompile(`[\[(]\s*[0-9]+(?:\.[0-9]+)?\s*,\s*([0-9]+(?:\.[0-9]+)?)\s*\]`)
	customArkAmount    = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
	// The date a model snapshot's ID ends with, as in doubao-seed-2-1-pro-260715.
	customArkSnapshotDate = regexp.MustCompile(`-[0-9]{6}$`)
	// An amount that follows a colon ("≤ 261 万像素：0.30"), which leaves the
	// pixel counts and ordinals of a cell out; and a cell that is one amount.
	customArkLabelledAmount = regexp.MustCompile(`[:：]\s*\$?\s*([0-9]+(?:\.[0-9]+)?)`)
	customArkPlainAmount    = regexp.MustCompile(`^\$?\s*([0-9]+(?:\.[0-9]+)?)$`)
	customArkResolution     = regexp.MustCompile(`(?i)\b([0-9]{3,4}p|4k)\b`)
)

// customArkTierRow is one row of a pricing table: a model's prices up to upper
// input tokens, 0 when the row has no input-length condition.
type customArkTierRow struct {
	model string
	upper float64
	tier  customVendorTier
	audio *float64
}

// readCustomArkTable reads the rows of one pricing table. columns names a
// header fragment per field; a model or provider cell left empty continues the
// row above. Rows conditioned on anything but the input length are dropped
// together with their model.
func readCustomArkTable(table customMarkdownTable, columns map[string]string) []customArkTierRow {
	index := map[string]int{"provider": -1, "model": -1, "condition": -1, "input": -1, "audio": -1, "cache": -1, "output": -1}
	for field, fragment := range columns {
		index[field] = customColumn(table.Header, fragment)
	}
	amount := func(row []string, field string) *float64 {
		match := customArkAmount.FindString(customCell(row, index[field]))
		value, err := strconv.ParseFloat(match, 64)
		if match == "" || err != nil {
			return nil
		}
		return &value
	}
	var rows []customArkTierRow
	dropped := make(map[string]bool)
	var model, provider string
	for _, row := range table.Rows {
		if name := strings.Trim(strings.ReplaceAll(customCell(row, index["model"]), `\`, ""), "` "); name != "" {
			model = strings.ToLower(name)
		}
		if owner := customCell(row, index["provider"]); owner != "" {
			provider = owner
		}
		if model == "" || (index["provider"] >= 0 && !strings.EqualFold(provider, "ByteDance")) {
			continue
		}
		entry := customArkTierRow{model: model, audio: amount(row, "audio")}
		condition := strings.ReplaceAll(customCell(row, index["condition"]), `\`, "")
		if bound := customArkTierBound.FindStringSubmatch(condition); bound != nil {
			thousands, _ := strconv.ParseFloat(bound[1], 64)
			entry.upper = thousands * 1000
		} else if condition != "" && condition != "-" && !strings.EqualFold(condition, "Not applicable") {
			dropped[model] = true
			continue
		}
		input := amount(row, "input")
		if input == nil {
			// The audio input table carries no ordinary input price.
			if entry.audio == nil {
				dropped[model] = true
				continue
			}
			rows = append(rows, entry)
			continue
		}
		entry.tier = customVendorTier{Input: *input, Output: amount(row, "output"), CacheRead: amount(row, "cache")}
		rows = append(rows, entry)
	}
	kept := rows[:0]
	for _, row := range rows {
		if !dropped[row.model] {
			kept = append(kept, row)
		}
	}
	return kept
}

// customArkVendorPrices turns table rows into list prices. perUSD is how many
// units of the page's currency one US dollar buys. The last tier of a model is
// open-ended, whatever context size the page gives as its bound.
func customArkVendorPrices(provider string, rows []customArkTierRow, perUSD float64) map[string]customVendorPrice {
	convert := func(value *float64) *float64 {
		if value == nil {
			return nil
		}
		converted := math.Round(*value/perUSD*1e6) / 1e6
		return &converted
	}
	tiers := make(map[string][]customVendorTier)
	for _, row := range rows {
		tier := row.tier
		tier.Upper = row.upper
		tier.Input = *convert(&tier.Input)
		tier.Output, tier.CacheRead, tier.AudioInput = convert(tier.Output), convert(tier.CacheRead), convert(row.audio)
		tiers[row.model] = append(tiers[row.model], tier)
	}
	prices := make(map[string]customVendorPrice, len(tiers))
	for model, list := range tiers {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Upper < list[j].Upper })
		// A model without length tiers has one row; several need a bound each.
		if len(list) > 1 && list[0].Upper == 0 {
			continue
		}
		list[len(list)-1].Upper = 0
		first := list[0]
		if first.Input <= 0 || first.Output == nil {
			continue
		}
		price := customVendorPrice{Provider: provider, Input: first.Input, Output: first.Output, CacheRead: first.CacheRead}
		if len(list) > 1 || first.AudioInput != nil {
			price.Tiers = list
		}
		prices[model] = price
	}
	return prices
}

// customArkModelName is the model a table cell names, without the markdown
// escapes, code marks and notes around it.
func customArkModelName(cell string) string {
	fields := strings.Fields(strings.NewReplacer(`\`, "", "`", "", "^", "").Replace(cell))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}

// customArkAmounts reads the prices of a cell: the cell itself when it is one
// amount, otherwise every amount that follows a colon.
func customArkAmounts(cell string) []float64 {
	cell = strings.TrimSpace(cell)
	matches := customArkLabelledAmount.FindAllStringSubmatch(cell, -1)
	if plain := customArkPlainAmount.FindStringSubmatch(cell); plain != nil {
		matches = [][]string{plain}
	}
	amounts := make([]float64, 0, len(matches))
	for _, match := range matches {
		if value, err := strconv.ParseFloat(match[1], 64); err == nil {
			amounts = append(amounts, value)
		}
	}
	return amounts
}

// customArkVideoTiers prices video tokens per million, once for every listed
// output resolution; without resolutions the price holds for every request.
func customArkVideoTiers(resolutions []string, videoInput string, perMillion float64) []customVendorTier {
	if len(resolutions) == 0 {
		return []customVendorTier{{Usage: map[string]float64{"tokens": perMillion}}}
	}
	tiers := make([]customVendorTier, 0, len(resolutions))
	for _, resolution := range resolutions {
		tiers = append(tiers, customVendorTier{
			When:  map[string]string{"resolution": strings.ToLower(resolution), "video_input": videoInput},
			Usage: map[string]float64{"tokens": perMillion},
		})
	}
	return tiers
}

// customArkImageTiers reads an image model's row: what an output image costs
// at or below 1.5K and above it, in layer decomposition and otherwise, and
// what an input image costs. A row of another shape yields nothing.
func customArkImageTiers(input, output string) []customVendorTier {
	prices := customArkAmounts(output)
	if len(prices) == 1 {
		prices = append(prices, prices[0])
	}
	layered := len(prices) == 4
	if layered {
		single := max(strings.Index(output, "单图"), strings.Index(output, "Single"))
		layers := max(strings.Index(output, "图层"), strings.Index(output, "Layer"))
		if single < 0 || layers < single {
			return nil
		}
	} else if len(prices) != 2 {
		return nil
	}
	inputKey, inputPrices := "input_images", customArkAmounts(input)
	if strings.Contains(input, "第 2 张") || strings.Contains(strings.ToLower(input), "second image") {
		inputKey = "input_images_after_first"
	}
	if len(inputPrices) > 1 {
		return nil
	}
	usage := func(lower, higher float64) map[string]float64 {
		facts := map[string]float64{"images_up_to_1_5k": lower, "images_above_1_5k": higher}
		if len(inputPrices) == 1 {
			facts[inputKey] = inputPrices[0]
		}
		return facts
	}
	if !layered {
		return []customVendorTier{{Usage: usage(prices[0], prices[1])}}
	}
	return []customVendorTier{
		{When: map[string]string{"layer_decomposition": "false"}, Usage: usage(prices[0], prices[1])},
		{When: map[string]string{"layer_decomposition": "true"}, Usage: usage(prices[2], prices[3])},
	}
}

// customArkUsagePrice is the list price of a task model from its usage tiers,
// converted from the page's currency. shown names the usage fact whose price
// in the first tier stands for the model where a single number is displayed.
func customArkUsagePrice(provider string, tiers []customVendorTier, perUSD float64, shown string) (customVendorPrice, bool) {
	if len(tiers) == 0 {
		return customVendorPrice{}, false
	}
	converted := make([]customVendorTier, len(tiers))
	for i, tier := range tiers {
		converted[i] = customVendorTier{When: tier.When, Usage: make(map[string]float64, len(tier.Usage))}
		for fact, price := range tier.Usage {
			converted[i].Usage[fact] = math.Round(price/perUSD*1e6) / 1e6
		}
	}
	price := customVendorPrice{Provider: provider, Input: converted[0].Usage[shown], Tiers: converted}
	return price, price.Input > 0
}

// customArkEmbeddingPrice prices an embedding model: text and image input per
// 1M tokens, and no output.
func customArkEmbeddingPrice(provider string, text, image, perUSD float64) customVendorPrice {
	free := 0.0
	input := math.Round(text/perUSD*1e6) / 1e6
	imageInput := math.Round(image/perUSD*1e6) / 1e6
	return customVendorPrice{Provider: provider, Input: input, Output: &free, Tiers: []customVendorTier{
		{Input: input, Output: &free, ImageInput: &imageInput},
	}}
}

// parseCustomBytePlusVendorPricing reads the Standard online inference table
// with the Standard audio input table beside it, the online video token prices
// with and without input video, the image prices and the embedding price. A
// model ID also answers to its name without the snapshot date while every
// snapshot of it costs the same.
func parseCustomBytePlusVendorPricing(page []byte) (map[string]customVendorPrice, error) {
	text := strings.ReplaceAll(string(page), "<br>", " ")
	var rows []customArkTierRow
	var audio []customArkTierRow
	video := make(map[string][]customVendorTier)
	prices := make(map[string]customVendorPrice)
	for _, table := range parseCustomMarkdownTables(text) {
		hasProvider := customColumn(table.Header, "provider") >= 0
		hasOutput := customColumn(table.Header, "output") >= 0
		videoInput := ""
		switch {
		case strings.Contains(table.Caption, "Input without video"):
			videoInput = "none"
		case strings.Contains(table.Caption, "Input with video"):
			videoInput = "video"
		}
		switch {
		case rows == nil && hasProvider && hasOutput:
			rows = readCustomArkTable(table, map[string]string{"provider": "provider", "model": "model id", "condition": "prompt length", "input": "input", "cache": "cached input", "output": "output"})
		case rows != nil && audio == nil && !hasProvider && !hasOutput && customColumn(table.Header, "model id") >= 0:
			audio = readCustomArkTable(table, map[string]string{"model": "model id", "condition": "prompt length", "audio": "input"})
		case videoInput != "" && len(table.Rows) > 1 && customColumn(table.Header, "online inference") == 1:
			// The first row names the resolutions of the online inference columns.
			labels := table.Rows[0]
			for _, row := range table.Rows[1:] {
				name := customArkModelName(customCell(row, 0))
				// One price across merged resolution cells holds for every resolution.
				if amount, ok := customFirstDollar(customCell(row, 1)); ok && videoInput == "none" && customCell(row, 2) == "" && customCell(row, 3) == "" {
					video[name] = customArkVideoTiers(nil, videoInput, amount)
					continue
				}
				for column := 1; column <= 3; column++ {
					if amount, ok := customFirstDollar(customCell(row, column)); ok {
						video[name] = append(video[name], customArkVideoTiers(customArkResolution.FindAllString(customCell(labels, column), -1), videoInput, amount)...)
					}
				}
			}
		case customColumn(table.Header, "input image") >= 0 && customColumn(table.Header, "output image") >= 0:
			for _, row := range table.Rows {
				if price, ok := customArkUsagePrice(customVendorBytePlusProvider, customArkImageTiers(customCell(row, 1), customCell(row, 2)), 1, "images_above_1_5k"); ok {
					prices[customArkModelName(customCell(row, 0))] = price
				}
			}
		}
	}
	// The embedding table puts its separator below its only row, so it is read
	// as lines: the model, the kinds of input and their prices in the same order.
	for line := range strings.SplitSeq(text, "\n") {
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) < 3 || !strings.Contains(cells[0], "embedding") {
			continue
		}
		kinds := strings.ToLower(cells[1])
		image, textInput := strings.Index(kinds, "image input"), strings.Index(kinds, "text input")
		amounts := customDollarAmount.FindAllStringSubmatch(cells[2], -1)
		if image < 0 || textInput < 0 || len(amounts) != 2 {
			continue
		}
		first, _ := strconv.ParseFloat(amounts[0][1], 64)
		second, _ := strconv.ParseFloat(amounts[1][1], 64)
		if image < textInput {
			first, second = second, first
		}
		prices[customArkModelName(cells[0])] = customArkEmbeddingPrice(customVendorBytePlusProvider, first, second, 1)
	}
	for i := range rows {
		for _, extra := range audio {
			if extra.model == rows[i].model && extra.upper == rows[i].upper {
				rows[i].audio = extra.audio
			}
		}
	}
	maps.Copy(prices, customArkVendorPrices(customVendorBytePlusProvider, rows, 1))
	for name, tiers := range video {
		if price, ok := customArkUsagePrice(customVendorBytePlusProvider, tiers, 1, "tokens"); ok {
			prices[name] = price
		}
	}
	delete(prices, "")
	undated := make(map[string]customVendorPrice)
	conflicting := make(map[string]bool)
	for model, price := range prices {
		family := customArkSnapshotDate.ReplaceAllString(model, "")
		if family == model {
			continue
		}
		if known, ok := undated[family]; ok && !reflect.DeepEqual(known, price) {
			conflicting[family] = true
		}
		undated[family] = price
	}
	for family, price := range undated {
		if _, listed := prices[family]; !listed && !conflicting[family] {
			prices[family] = price
		}
	}
	return customPageResult(prices)
}

// parseCustomVolcenginePricing reads the standard online inference table (the
// first one of the page), the online video token prices, the image prices and
// the embedding price, and converts their CNY at the rate set for vendor prices
// (CNY per USD). Only doubao models are kept. The page writes a version as
// "2.1" while model IDs write "2-1", so each model answers to both.
func parseCustomVolcenginePricing(page []byte) (map[string]customVendorPrice, error) {
	common.OptionMapRWMutex.RLock()
	rate, err := strconv.ParseFloat(strings.TrimSpace(common.OptionMap[customVendorCNYPerUSDOption]), 64)
	common.OptionMapRWMutex.RUnlock()
	if err != nil || rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return nil, errors.New("Volcengine lists prices in CNY: set the CNY per USD rate for vendor prices under Billing > Upstream pricing config")
	}
	provider := fmt.Sprintf("%s (CNY at %s per USD)", customVendorVolcengineProvider, strconv.FormatFloat(rate, 'f', -1, 64))
	found := make(map[string]customVendorPrice)
	chat := false
	for _, table := range parseCustomMarkdownTables(strings.ReplaceAll(string(page), "<br>", " ")) {
		switch {
		case !chat && customColumn(table.Header, "模型名称") >= 0 && customColumn(table.Header, "输出") >= 0 && customColumn(table.Header, "条件") >= 0:
			chat = true
			rows := readCustomArkTable(table, map[string]string{"model": "模型名称", "condition": "条件", "input": "输入(非音频)", "audio": "输入(音频)", "cache": "缓存命中(非音频)", "output": "输出"})
			maps.Copy(found, customArkVendorPrices(provider, rows, rate))
		case customColumn(table.Header, "在线推理") >= 0:
			for _, row := range table.Rows {
				online := customCell(row, customColumn(table.Header, "在线推理"))
				var tiers []customVendorTier
				if flat := customArkAmounts(online); len(flat) == 1 && !strings.Contains(online, "输出视频分辨率为") {
					tiers = customArkVideoTiers(nil, "", flat[0])
				}
				// "输出视频分辨率为 480p，720p … 输入不含视频：46.00 … 输入包含视频：28.00", once per group of resolutions.
				for _, group := range strings.Split(online, "输出视频分辨率为")[1:] {
					resolutions, _, _ := strings.Cut(group, "输入")
					for _, kind := range [][2]string{{"输入不含视频", "none"}, {"输入包含视频", "video"}} {
						if _, after, ok := strings.Cut(group, kind[0]); ok {
							if amount := customArkLabelledAmount.FindStringSubmatch(after); amount != nil {
								value, _ := strconv.ParseFloat(amount[1], 64)
								tiers = append(tiers, customArkVideoTiers(customArkResolution.FindAllString(resolutions, -1), kind[1], value)...)
							}
						}
					}
				}
				if price, ok := customArkUsagePrice(provider, tiers, rate, "tokens"); ok {
					found[customArkModelName(customCell(row, 0))] = price
				}
			}
		case customColumn(table.Header, "输入图单价") >= 0 && customColumn(table.Header, "输出图单价") >= 0:
			for _, row := range table.Rows {
				if price, ok := customArkUsagePrice(provider, customArkImageTiers(customCell(row, 1), customCell(row, 2)), rate, "images_above_1_5k"); ok {
					found[customArkModelName(customCell(row, 0))] = price
				}
			}
		case customColumn(table.Header, "文本输入") >= 0 && customColumn(table.Header, "图片输入") >= 0:
			for _, row := range table.Rows {
				text, image := customArkAmounts(customCell(row, 1)), customArkAmounts(customCell(row, 2))
				if len(text) == 1 && len(image) == 1 {
					found[customArkModelName(customCell(row, 0))] = customArkEmbeddingPrice(provider, text[0], image[0], rate)
				}
			}
		}
	}
	prices := make(map[string]customVendorPrice)
	for model, price := range found {
		if !strings.HasPrefix(model, "doubao-") || strings.ContainsAny(model, " >") {
			continue
		}
		prices[model] = price
		prices[strings.ReplaceAll(model, ".", "-")] = price
	}
	return customPageResult(prices)
}
