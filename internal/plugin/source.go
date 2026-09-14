package plugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	starRailGameID       = 6
	starRailGuideForumID = 61
	searchEndpoint       = "https://bbs-api.miyoushe.com/post/wapi/searchPosts"
	detailEndpoint       = "https://bbs-api.miyoushe.com/post/wapi/getPostFull"
)

var (
	imageHosts = map[string]struct{}{
		"bbs-static.miyoushe.com": {},
		"upload-bbs.mihoyo.com":   {},
		"upload-bbs.miyoushe.com": {},
	}
	imageTagPattern = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)
	requestHeaders  = map[string]string{
		"Accept":            "application/json",
		"Referer":           "https://www.miyoushe.com/sr/",
		"User-Agent":        "Mozilla/5.0 RayleaBot/0.2 game-guide",
		"x-rpc-app_version": "2.83.1",
		"x-rpc-client_type": "4",
	}
)

type guideSource struct {
	PostID string        `json:"post_id"`
	Title  string        `json:"title"`
	Author string        `json:"author"`
	URL    string        `json:"url"`
	Images []sourceImage `json:"images"`
}

type sourceImage struct {
	URL  string `json:"url"`
	File string `json:"file"`
}

func (service *guideService) searchSources(ctx context.Context, item character) []guideSource {
	terms := append([]string{item.Name}, item.Aliases...)
	if len(terms) > 3 {
		terms = terms[:3]
	}
	seenPosts := map[string]struct{}{}
	seenImages := map[string]struct{}{}
	var sources []guideSource
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		params := url.Values{}
		params.Set("gids", strconv.Itoa(starRailGameID))
		params.Set("keyword", term+"攻略")
		params.Set("page_size", strconv.Itoa(max(service.maxSources*4, 8)))
		result, err := service.actions.HTTPRequest(ctx, httpRequest{
			Method: "GET", URL: searchEndpoint + "?" + params.Encode(), Headers: cloneHeaders(requestHeaders), TimeoutSeconds: 15,
		})
		if err != nil || !successfulResponse(result) {
			reason := requestFailureReason(err, result)
			service.log(ctx, "debug", fmt.Sprintf("“%s”攻略搜索失败，尝试其他关键词或缓存：%s", item.Name, reason), map[string]any{"character": item.Name, "term": term, "status_code": intValue(result["status_code"]), "error": errorText(err)})
			continue
		}
		document := responseDocument(result)
		for _, source := range sourcesFromDocument(document, item) {
			if _, exists := seenPosts[source.PostID]; exists {
				continue
			}
			if detailImages := service.fetchPostImages(ctx, source.PostID); len(detailImages) > 0 {
				source.Images = make([]sourceImage, 0, len(detailImages))
				for _, imageURL := range detailImages {
					source.Images = append(source.Images, sourceImage{URL: imageURL})
				}
			}
			filtered := make([]sourceImage, 0, len(source.Images))
			for _, image := range source.Images {
				if _, exists := seenImages[image.URL]; exists {
					continue
				}
				seenImages[image.URL] = struct{}{}
				filtered = append(filtered, image)
			}
			if len(filtered) == 0 {
				continue
			}
			source.Images = filtered
			sources = append(sources, source)
			seenPosts[source.PostID] = struct{}{}
			if len(sources) >= service.maxSources {
				return sources
			}
		}
		if len(sources) > 0 {
			return sources
		}
	}
	return sources
}

func (service *guideService) fetchPostImages(ctx context.Context, postID string) []string {
	postID = strings.TrimSpace(postID)
	if postID == "" {
		return nil
	}
	params := url.Values{"post_id": []string{postID}}
	result, err := service.actions.HTTPRequest(ctx, httpRequest{
		Method: "GET", URL: detailEndpoint + "?" + params.Encode(), Headers: cloneHeaders(requestHeaders), TimeoutSeconds: 15,
	})
	if err != nil || !successfulResponse(result) {
		reason := requestFailureReason(err, result)
		service.log(ctx, "warn", fmt.Sprintf("米游社攻略帖子 %s 读取失败，已跳过：%s", postID, reason), map[string]any{"post_id": postID, "status_code": intValue(result["status_code"]), "error": errorText(err)})
		return nil
	}
	return imageURLsFromDetail(responseDocument(result))
}

func sourcesFromDocument(document map[string]any, item character) []guideSource {
	posts := anySlice(document["posts"])
	if len(posts) == 0 {
		posts = anySlice(document["list"])
	}
	result := make([]guideSource, 0, len(posts))
	for _, raw := range posts {
		bundle, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		post := mapValue(bundle["post"])
		if len(post) == 0 {
			post = bundle
		}
		if intValue(post["game_id"]) != starRailGameID {
			continue
		}
		forumID := intValue(post["f_forum_id"])
		if forumID != 0 && forumID != starRailGuideForumID {
			continue
		}
		if !postMatchesCharacter(post, item) {
			continue
		}
		images := imageURLsFromPost(post)
		if len(images) == 0 {
			continue
		}
		postID := stringValue(post["post_id"])
		source := guideSource{
			PostID: postID,
			Title:  stringValue(post["subject"]),
			Author: stringValue(mapValue(bundle["user"])["nickname"]),
			Images: make([]sourceImage, 0, len(images)),
		}
		if postID != "" {
			source.URL = "https://www.miyoushe.com/sr/article/" + url.PathEscape(postID)
		}
		for _, imageURL := range images {
			source.Images = append(source.Images, sourceImage{URL: imageURL})
		}
		result = append(result, source)
	}
	return result
}

func postMatchesCharacter(post map[string]any, item character) bool {
	haystack := normalize(stringValue(post["subject"]) + " " + stringValue(post["content"]))
	if !strings.Contains(haystack, normalize("攻略")) {
		return false
	}
	terms := append([]string{item.Name}, item.Aliases...)
	for _, term := range terms {
		if key := normalize(term); key != "" && strings.Contains(haystack, key) {
			return true
		}
	}
	return false
}

func imageURLsFromPost(post map[string]any) []string {
	var result []string
	for _, raw := range anySlice(post["images"]) {
		addImageURL(&result, stringValue(raw))
	}
	if len(result) == 0 {
		addImageURL(&result, stringValue(post["cover"]))
	}
	return result
}

func imageURLsFromDetail(document map[string]any) []string {
	bundle := mapValue(document["post"])
	post := mapValue(bundle["post"])
	if len(post) == 0 {
		post = bundle
	}
	var result []string
	for _, raw := range anySlice(post["images"]) {
		addImageURL(&result, stringValue(raw))
	}
	for _, raw := range anySlice(bundle["image_list"]) {
		addImageURL(&result, stringValue(mapValue(raw)["url"]))
	}
	if structured := stringValue(post["structured_content"]); structured != "" {
		var items []map[string]any
		if json.Unmarshal([]byte(structured), &items) == nil {
			for _, entry := range items {
				addImageURL(&result, stringValue(mapValue(entry["insert"])["image"]))
			}
		}
	}
	for _, match := range imageTagPattern.FindAllStringSubmatch(stringValue(post["content"]), -1) {
		if len(match) == 2 {
			addImageURL(&result, match[1])
		}
	}
	addImageURL(&result, stringValue(post["cover"]))
	addImageURL(&result, stringValue(mapValue(bundle["cover"])["url"]))
	return result
}

func addImageURL(urls *[]string, candidate string) {
	candidate = strings.TrimSpace(candidate)
	parsed, err := url.Parse(candidate)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || !supportedImageHost(parsed.Hostname()) {
		return
	}
	for _, existing := range *urls {
		if existing == candidate {
			return
		}
	}
	*urls = append(*urls, candidate)
}

func supportedImageHost(host string) bool {
	_, ok := imageHosts[strings.ToLower(strings.TrimSpace(host))]
	return ok
}

func responseDocument(result rayleabot.ActionResult) map[string]any {
	var envelope map[string]any
	if json.Unmarshal([]byte(responseText(result)), &envelope) != nil {
		return map[string]any{}
	}
	retcode, hasRetcode := envelope["retcode"]
	if !hasRetcode || intValue(retcode) != 0 {
		return map[string]any{}
	}
	return mapValue(envelope["data"])
}

func responseText(result rayleabot.ActionResult) string {
	if text, ok := result["body_text"].(string); ok {
		return text
	}
	encoded, _ := result["body_base64"].(string)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func responseBytes(result rayleabot.ActionResult) []byte {
	if encoded, ok := result["body_base64"].(string); ok && encoded != "" {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err == nil {
			return decoded
		}
	}
	if text, ok := result["body_text"].(string); ok {
		return []byte(text)
	}
	return nil
}

func successfulResponse(result rayleabot.ActionResult) bool {
	status := intValue(result["status_code"])
	return status >= 200 && status < 300
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		result, _ := typed.Int64()
		return int(result)
	case string:
		result, _ := strconv.Atoi(typed)
		return result
	default:
		return 0
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func mapValue(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil {
		return map[string]any{}
	}
	return result
}

func anySlice(value any) []any {
	result, _ := value.([]any)
	return result
}

func cloneHeaders(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func errorText(err error) string {
	if err == nil {
		return "unexpected response"
	}
	return err.Error()
}

func requestFailureReason(err error, result rayleabot.ActionResult) string {
	if err != nil {
		return err.Error()
	}
	if status := intValue(result["status_code"]); status > 0 {
		return fmt.Sprintf("上游返回 HTTP %d", status)
	}
	return "上游响应没有成功状态"
}
