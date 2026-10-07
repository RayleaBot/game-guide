package plugin

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/game-guide/internal/assets"
)

type character struct {
	Name    string   `json:"name"`
	Slug    string   `json:"slug"`
	Aliases []string `json:"aliases"`
	Matched bool     `json:"-"`
}

type characterCatalog struct {
	Characters []character `json:"characters"`
}

type requestKind int

const (
	requestNone requestKind = iota
	requestGuide
	requestCharacterList
)

type guideRequest struct {
	Kind  requestKind
	Query string
}

var pinyinInitials = map[rune]string{
	'阿': "A", '艾': "A", '白': "B", '波': "B", '不': "B", '布': "B",
	'大': "D", '丹': "D", '飞': "F", '绯': "F", '翡': "F", '风': "F", '符': "F",
	'桂': "G", '海': "H", '寒': "H", '黑': "H", '虎': "H", '花': "H", '黄': "H", '火': "H", '藿': "H",
	'姬': "J", '加': "J", '椒': "J", '杰': "J", '景': "J", '镜': "J",
	'卡': "K", '开': "K", '克': "K", '刻': "K",
	'灵': "L", '玲': "L", '流': "L", '卢': "L", '乱': "L", '罗': "L",
	'米': "M", '貊': "M", '那': "N", '娜': "N", '佩': "P",
	'千': "Q", '青': "Q", '刃': "R", '阮': "R",
	'赛': "S", '三': "S", '桑': "S", '砂': "S", '素': "S",
	'缇': "T", '停': "T", '托': "T", '瓦': "W", '万': "W", '忘': "W",
	'希': "X", '昔': "X", '遐': "X", '星': "X", '雪': "X",
	'彦': "Y", '爻': "Y", '银': "Y", '驭': "Y", '云': "Y",
	'长': "Z", '真': "Z", '知': "Z",
}

func parseRequest(event rayleabot.Event, commandPrefixes []string) guideRequest {
	command := strings.TrimSpace(event.Command())
	if command == "角色列表" {
		return guideRequest{Kind: requestCharacterList}
	}
	if command == "游戏攻略" {
		query := strings.TrimSpace(strings.Join(event.Args(), " "))
		if query != "" {
			return guideRequest{Kind: requestGuide, Query: query}
		}
		return guideRequest{}
	}
	if strings.HasSuffix(command, "攻略") {
		query := strings.TrimSpace(strings.TrimSuffix(command, "攻略"))
		if query != "" {
			return guideRequest{Kind: requestGuide, Query: query}
		}
	}

	body, ok := prefixedBody(event.Message.PlainText, commandPrefixes)
	if !ok {
		return guideRequest{}
	}
	if body == "角色列表" {
		return guideRequest{Kind: requestCharacterList}
	}
	if strings.HasSuffix(body, "攻略") {
		query := strings.TrimSpace(strings.TrimSuffix(body, "攻略"))
		if query != "" {
			return guideRequest{Kind: requestGuide, Query: query}
		}
	}
	return guideRequest{}
}

func prefixedBody(text string, commandPrefixes []string) (string, bool) {
	prefixes := append([]string{"*", "＊"}, commandPrefixes...)
	sort.SliceStable(prefixes, func(i, j int) bool { return len(prefixes[i]) > len(prefixes[j]) })
	text = strings.TrimSpace(text)
	for _, prefix := range prefixes {
		if prefix != "" && strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(text, prefix)), true
		}
	}
	return "", false
}

func resolveCharacter(query string) character {
	key := normalize(query)
	for _, item := range catalog.Characters {
		if normalize(item.Name) == key || normalize(item.Slug) == key {
			item.Matched = true
			return item
		}
		for _, alias := range item.Aliases {
			if normalize(alias) == key {
				item.Matched = true
				return item
			}
		}
	}
	fallback := strings.TrimSpace(query)
	return character{Name: fallback, Slug: safeSlug(fallback), Matched: false}
}

func findCharacter(query string) (character, bool) {
	item := resolveCharacter(query)
	return item, item.Matched
}

func normalize(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("-_:：，,。.!！?？()[]{}<>《》【】\"'“”‘’/\\·•・&", r) {
			return -1
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(value))
}

func safeSlug(value string) string {
	key := normalize(value)
	var builder strings.Builder
	for _, r := range key {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.In(r, unicode.Han) {
			builder.WriteRune(r)
		} else if builder.Len() > 0 {
			builder.WriteByte('-')
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		digest := sha1.Sum([]byte(value))
		result = hex.EncodeToString(digest[:])[:12]
	}
	runes := []rune(result)
	if len(runes) > 80 {
		result = string(runes[:80])
	}
	return result
}

func characterListRenderData(characters []character) map[string]any {
	grouped := map[string][]character{}
	for _, item := range characters {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		key := characterGroupKey(item.Name)
		grouped[key] = append(grouped[key], item)
	}
	labels := make([]string, 0, len(grouped))
	for label := range grouped {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	groups := make([]map[string]any, 0, len(labels))
	total := 0
	for _, label := range labels {
		items := grouped[label]
		sort.SliceStable(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		characters := make([]map[string]any, 0, len(items))
		for _, item := range items {
			aliases := make([]string, 0, len(item.Aliases))
			for _, alias := range item.Aliases {
				if alias = strings.TrimSpace(alias); alias != "" {
					aliases = append(aliases, alias)
				}
			}
			characters = append(characters, map[string]any{"name": item.Name, "slug": item.Slug, "aliases": aliases})
		}
		total += len(characters)
		groups = append(groups, map[string]any{"label": label, "characters": characters})
	}
	return map[string]any{
		"title":    "星穹铁道角色目录",
		"subtitle": fmt.Sprintf("已适配 %d 名角色 · 名称与别名均可用于查询", total),
		"total":    total,
		"groups":   groups,
	}
}

func characterGroupKey(name string) string {
	for _, first := range strings.TrimSpace(name) {
		if mapped := pinyinInitials[first]; mapped != "" {
			return mapped
		}
		if first >= 'a' && first <= 'z' {
			return strings.ToUpper(string(first))
		}
		if first >= 'A' && first <= 'Z' {
			return string(first)
		}
		return "#"
	}
	return "#"
}

func mustCharacterCatalog() characterCatalog {
	var value characterCatalog
	if err := json.Unmarshal(assets.CharacterData, &value); err != nil || len(value.Characters) == 0 {
		panic("invalid embedded character catalog")
	}
	return value
}
