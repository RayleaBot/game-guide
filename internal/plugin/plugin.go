package plugin

import (
	"context"
	"fmt"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

var catalog = mustCharacterCatalog()

func Run(ctx context.Context) error {
	return rayleabot.Run(ctx, rayleabot.Options{}, rayleabot.HandlerFunc(handleEvent))
}

func handleEvent(ctx context.Context, event *rayleabot.EventContext) error {
	request := parseRequest(event.Event, event.CommandPrefixes)
	if request.Kind == requestNone {
		return event.Result(map[string]any{"handled": false})
	}
	if request.Kind == requestCharacterList {
		return sendCharacterList(ctx, event)
	}
	matched := resolveCharacter(request.Query)
	return newGuideService(runtimeActions{Actions: event.Actions()}).send(ctx, event, matched, request.Query)
}

func sendCharacterList(ctx context.Context, event *rayleabot.EventContext) error {
	data := characterListRenderData(catalog.Characters)
	fallback := "当前已适配 " + fmt.Sprint(data["total"]) + " 名角色，发送「*角色名攻略」查看角色攻略图。"
	result, err := event.Actions().RenderImage(ctx, rayleabot.RenderImageRequest{
		Template: "character-list", Data: data, Output: "png", FallbackText: fallback,
	})
	if err != nil {
		logAction(ctx, event.Actions(), "warn", fmt.Sprintf("包含 %v 名角色的攻略列表图片渲染失败；本次将返回文字错误提示。原因：%s", data["total"], err.Error()), map[string]any{"character_count": data["total"], "error": err.Error()})
		return event.SendText("角色列表图片生成失败，请稍后再试。")
	}
	imagePath, _ := result["image_path"].(string)
	if strings.TrimSpace(imagePath) == "" {
		logAction(ctx, event.Actions(), "warn", fmt.Sprintf("包含 %v 名角色的攻略列表渲染完成，但结果没有图片路径；本次将返回文字错误提示。", data["total"]), map[string]any{"character_count": data["total"], "has_image_path": false})
		return event.SendText("角色列表图片生成失败，请稍后再试。")
	}
	_, err = event.Actions().MessageSend(ctx, rayleabot.MessageSendRequest{
		TargetType: event.Event.Target.Type,
		TargetID:   event.Event.Target.ID,
		Message:    rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Image(imagePath)}},
	})
	if err != nil {
		logAction(ctx, event.Actions(), "warn", fmt.Sprintf("攻略角色列表图片发送到 %s %s 失败；用户未收到列表，请稍后重试。原因：%s", event.Event.Target.Type, event.Event.Target.ID, err.Error()), mergeFields(map[string]any{
			"target_type": event.Event.Target.Type, "target_id": event.Event.Target.ID,
		}, actionErrorFields(err)))
		return event.SendText("角色列表图片发送失败，请稍后再试。")
	}
	return event.Result(map[string]any{"handled": true, "command": "character-list", "total": data["total"]})
}
