package plugin

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	defaultMaxSources         = 4
	defaultMaxImagesPerSource = 60
	defaultMaxTotalImages     = 120
	forwardImagesPerMessage   = 100
)

type guideActions interface {
	HTTPRequest(context.Context, httpRequest) (rayleabot.ActionResult, error)
	FileRead(context.Context, string) (rayleabot.ActionResult, error)
	FileWriteText(context.Context, string, string) (rayleabot.ActionResult, error)
	FileWriteBase64(context.Context, string, string) (rayleabot.ActionResult, error)
	FileList(context.Context, string) (rayleabot.ActionResult, error)
	MessageSend(context.Context, rayleabot.MessageSendRequest) (rayleabot.ActionResult, error)
	MessageForwardSend(context.Context, rayleabot.MessageForwardSendRequest) (rayleabot.ActionResult, error)
	LoggerWrite(context.Context, rayleabot.LoggerWriteRequest) (rayleabot.ActionResult, error)
}

type loggerActions interface {
	LoggerWrite(context.Context, rayleabot.LoggerWriteRequest) (rayleabot.ActionResult, error)
}

type guideService struct {
	actions            guideActions
	maxSources         int
	maxImagesPerSource int
	maxTotalImages     int
}

type guideImage struct {
	File string
	Path string
}

type cacheRecord struct {
	SchemaVersion int           `json:"schema_version"`
	Game          string        `json:"game"`
	Character     string        `json:"character"`
	UpdatedAt     string        `json:"updated_at"`
	Sources       []guideSource `json:"sources"`
}

func newGuideService(actions guideActions) *guideService {
	return &guideService{
		actions: actions, maxSources: defaultMaxSources,
		maxImagesPerSource: defaultMaxImagesPerSource, maxTotalImages: defaultMaxTotalImages,
	}
}

func (service *guideService) send(ctx context.Context, event *rayleabot.EventContext, item character, requested string) error {
	if strings.TrimSpace(item.Name) == "" {
		return event.SendText("请在攻略前写角色名，例如「*昔涟攻略」。")
	}
	service.sendFetchingNotice(ctx, event, item)
	service.log(ctx, "debug", fmt.Sprintf("正在查询“%s”攻略", item.Name), map[string]any{
		"query": requested, "character": item.Name, "matched_alias": item.Matched,
		"target_type": event.Event.Target.Type, "target_id": event.Event.Target.ID,
	})

	images, fromCache := service.loadCachedImages(ctx, item)
	if len(images) == 0 {
		legacyImages := service.scanCachedImages(ctx, item)
		images = service.refreshCache(ctx, item)
		if len(images) == 0 && len(legacyImages) > 0 {
			images, fromCache = legacyImages, true
		}
	} else {
		service.log(ctx, "debug", fmt.Sprintf("使用“%s”攻略缓存，共 %d 张图片", item.Name, len(images)), map[string]any{"character": item.Name, "images": len(images)})
	}
	if len(images) == 0 {
		service.log(ctx, "warn", fmt.Sprintf("未找到“%s”攻略图片。", item.Name), map[string]any{"character": item.Name})
		return event.SendText("没有找到「" + item.Name + "」的星穹铁道攻略图。")
	}
	if err := service.sendImages(ctx, event, item, images, fromCache); err != nil {
		service.log(ctx, "warn", fmt.Sprintf("“%s”攻略图片未全部确认送达：%s", item.Name, err.Error()), mergeFields(map[string]any{
			"character": item.Name, "delivery_kind": "message.forward.send",
			"target_type": event.Event.Target.Type, "target_id": event.Event.Target.ID,
			"images": len(images), "from_cache": fromCache,
		}, actionErrorFields(err)))
		return event.SendText("攻略图发送失败，请稍后重试。")
	}
	return event.Result(map[string]any{
		"handled": true, "character": item.Name, "images": len(images), "from_cache": fromCache,
	})
}

func (service *guideService) sendFetchingNotice(ctx context.Context, event *rayleabot.EventContext, item character) {
	progressCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := service.actions.MessageSend(progressCtx, rayleabot.MessageSendRequest{
		TargetType: event.Event.Target.Type,
		TargetID:   event.Event.Target.ID,
		Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{
			rayleabot.Text("收到，正在获取「" + item.Name + "」攻略图，请稍候…"),
		}},
	})
	if err != nil {
		service.log(ctx, "warn", fmt.Sprintf("“%s”查询提示发送失败，继续查询攻略：%s", item.Name, err.Error()), mergeFields(map[string]any{
			"character": item.Name, "target_type": event.Event.Target.Type, "target_id": event.Event.Target.ID,
		}, actionErrorFields(err)))
	}
}

func (service *guideService) loadCachedImages(ctx context.Context, item character) ([]guideImage, bool) {
	result, err := service.actions.FileRead(ctx, service.indexPath(item))
	if err != nil || !boolValue(result["exists"]) {
		return nil, false
	}
	var record cacheRecord
	if json.Unmarshal([]byte(stringValue(result["content_text"])), &record) != nil {
		return nil, false
	}
	images := make([]guideImage, 0)
	for _, source := range record.Sources {
		for _, cached := range source.Images {
			image, ok := service.readCachedImage(ctx, cached.File)
			if ok {
				images = append(images, image)
			}
			if len(images) >= service.maxTotalImages {
				return images, true
			}
		}
	}
	return images, len(images) > 0
}

func (service *guideService) scanCachedImages(ctx context.Context, item character) []guideImage {
	prefix := service.guideDir(item) + "/"
	result, err := service.actions.FileList(ctx, prefix)
	if err != nil {
		return nil
	}
	paths := stringSlice(result["paths"])
	sort.Strings(paths)
	images := make([]guideImage, 0, len(paths))
	for _, cachedPath := range paths {
		if cachedPath == service.indexPath(item) || !supportedCacheExtension(cachedPath) {
			continue
		}
		if image, ok := service.readCachedImage(ctx, cachedPath); ok {
			images = append(images, image)
		}
		if len(images) >= service.maxTotalImages {
			break
		}
	}
	return images
}

func (service *guideService) readCachedImage(ctx context.Context, cachedPath string) (guideImage, bool) {
	result, err := service.actions.FileRead(ctx, cachedPath)
	if err != nil || !boolValue(result["exists"]) {
		return guideImage{}, false
	}
	encoded, _ := result["content_base64"].(string)
	if encoded == "" {
		if text, ok := result["content_text"].(string); ok && text != "" {
			encoded = base64.StdEncoding.EncodeToString([]byte(text))
		}
	}
	if encoded == "" {
		return guideImage{}, false
	}
	return guideImage{File: "base64://" + encoded, Path: cachedPath}, true
}

func (service *guideService) refreshCache(ctx context.Context, item character) []guideImage {
	sources := service.searchSources(ctx, item)
	if len(sources) == 0 {
		return nil
	}
	record := cacheRecord{
		SchemaVersion: 1, Game: "honkai_star_rail", Character: item.Name,
		UpdatedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
		Sources:   make([]guideSource, 0, len(sources)),
	}
	images := make([]guideImage, 0)
	for sourceIndex, source := range sources {
		candidates := source.Images
		if len(candidates) > service.maxImagesPerSource {
			candidates = candidates[:service.maxImagesPerSource]
		}
		stored := source
		stored.Images = nil
		for imageIndex, candidate := range candidates {
			content := service.downloadImage(ctx, candidate.URL)
			if len(content) == 0 {
				continue
			}
			cachedPath := service.imagePath(item, sourceIndex, source.PostID, imageIndex, candidate.URL)
			encoded := base64.StdEncoding.EncodeToString(content)
			if _, err := service.actions.FileWriteBase64(ctx, cachedPath, encoded); err != nil {
				service.log(ctx, "warn", fmt.Sprintf("“%s”攻略图片保存失败，已跳过这张图片：%s", item.Name, err.Error()), mergeFields(map[string]any{"character": item.Name, "path": cachedPath}, actionErrorFields(err)))
				continue
			}
			stored.Images = append(stored.Images, sourceImage{URL: candidate.URL, File: cachedPath})
			images = append(images, guideImage{File: "base64://" + encoded, Path: cachedPath})
			if len(images) >= service.maxTotalImages {
				break
			}
		}
		if len(stored.Images) > 0 {
			record.Sources = append(record.Sources, stored)
		}
		if len(images) >= service.maxTotalImages {
			break
		}
	}
	if len(images) > 0 {
		encoded, _ := json.MarshalIndent(record, "", "  ")
		if _, err := service.actions.FileWriteText(ctx, service.indexPath(item), string(encoded)+"\n"); err != nil {
			indexPath := service.indexPath(item)
			service.log(ctx, "warn", fmt.Sprintf("“%s”攻略缓存记录保存失败，下次可能重新下载：%s", item.Name, err.Error()), mergeFields(map[string]any{"character": item.Name, "path": indexPath}, actionErrorFields(err)))
		}
	}
	return images
}

func (service *guideService) downloadImage(ctx context.Context, imageURL string) []byte {
	parsed, err := url.Parse(imageURL)
	if err != nil || !supportedImageHost(parsed.Hostname()) {
		return nil
	}
	result, err := service.actions.HTTPRequest(ctx, httpRequest{
		Method: "GET", URL: imageURL, TimeoutSeconds: 30,
		Headers: map[string]string{
			"Accept":  "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8",
			"Referer": "https://www.miyoushe.com/sr/", "User-Agent": requestHeaders["User-Agent"],
		},
	})
	if err != nil || !successfulResponse(result) {
		return nil
	}
	return responseBytes(result)
}

func (service *guideService) sendImages(ctx context.Context, event *rayleabot.EventContext, item character, images []guideImage, fromCache bool) error {
	uin := strings.TrimSpace(event.Bot.ID)
	if uin == "" {
		uin = "10000"
	}
	for start := 0; start < len(images); start += forwardImagesPerMessage {
		end := min(start+forwardImagesPerMessage, len(images))
		messages := make([]rayleabot.ForwardMessage, 0, end-start)
		for _, image := range images[start:end] {
			messages = append(messages, rayleabot.ForwardMessage{
				"type": "node",
				"data": map[string]any{
					"name": item.Name + "攻略", "uin": uin,
					"content": []map[string]any{{"type": "image", "data": map[string]any{"file": image.File}}},
				},
			})
		}
		batchCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		_, err := service.actions.MessageForwardSend(batchCtx, rayleabot.MessageForwardSendRequest{
			TargetType: rayleabot.ConversationType(event.Event.Target.Type),
			TargetID:   event.Event.Target.ID,
			Messages:   messages,
		})
		cancel()
		if err != nil {
			return fmt.Errorf("send guide batch %d: %w", start/forwardImagesPerMessage+1, err)
		}
	}
	service.log(ctx, "info", fmt.Sprintf("“%s”攻略已发送，共 %d 张图片。", item.Name, len(images)), map[string]any{
		"character": item.Name, "images": len(images),
		"batches":    (len(images) + forwardImagesPerMessage - 1) / forwardImagesPerMessage,
		"from_cache": fromCache, "target_type": event.Event.Target.Type, "target_id": event.Event.Target.ID,
	})
	return nil
}

func (service *guideService) indexPath(item character) string {
	return service.guideDir(item) + "/index.json"
}

func (service *guideService) guideDir(item character) string {
	return "guides/" + safeSlug(item.Slug)
}

func (service *guideService) imagePath(item character, sourceIndex int, postID string, imageIndex int, imageURL string) string {
	checksum := sha1.Sum([]byte(imageURL))
	digest := hex.EncodeToString(checksum[:])[:12]
	return fmt.Sprintf("%s/%02d_%s_%02d_%s%s", service.guideDir(item), sourceIndex+1,
		safeSlug(defaultString(postID, "post")), imageIndex+1, digest, imageExtension(imageURL))
}

func imageExtension(imageURL string) string {
	parsed, _ := url.Parse(imageURL)
	extension := strings.ToLower(path.Ext(parsed.Path))
	switch extension {
	case ".gif", ".jpg", ".png", ".webp":
		return extension
	case ".jpeg":
		return ".jpg"
	default:
		return ".jpg"
	}
}

func supportedCacheExtension(filePath string) bool {
	switch strings.ToLower(path.Ext(filePath)) {
	case ".gif", ".jpg", ".jpeg", ".png", ".webp":
		return true
	default:
		return false
	}
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func stringSlice(value any) []string {
	if typed, ok := value.([]string); ok {
		return append([]string(nil), typed...)
	}
	result := make([]string, 0)
	for _, item := range anySlice(value) {
		if text := stringValue(item); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (service *guideService) log(ctx context.Context, level, message string, fields map[string]any) {
	logAction(ctx, service.actions, level, message, fields)
}

func logAction(ctx context.Context, actions loggerActions, level, message string, fields map[string]any) {
	if actions == nil {
		return
	}
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: level, Message: message, Fields: fields})
}

func actionErrorFields(err error) map[string]any {
	if err == nil {
		return map[string]any{}
	}
	fields := map[string]any{"error": err.Error()}
	var actionErr *rayleabot.ActionError
	if errors.As(err, &actionErr) {
		fields["error_code"] = actionErr.Code
		if len(actionErr.Details) > 0 {
			fields["error_details"] = actionErr.Details
		}
	}
	return fields
}

func mergeFields(left, right map[string]any) map[string]any {
	for key, value := range right {
		left[key] = value
	}
	return left
}
