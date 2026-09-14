package plugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func TestFindCharacterByNameAliasAndSlug(t *testing.T) {
	item := catalog.Characters[0]
	for _, candidate := range append([]string{item.Name, item.Slug}, item.Aliases...) {
		if candidate == "" {
			continue
		}
		got, ok := findCharacter(candidate)
		if !ok || got.Name != item.Name {
			t.Fatalf("findCharacter(%q) = %#v, %v", candidate, got, ok)
		}
	}
}

func TestNormalizeIgnoresGuideSeparators(t *testing.T) {
	if normalize("The Herta") != normalize("the-herta") {
		t.Fatal("expected equivalent normalized names")
	}
}

func TestParseRequestRequiresPrefixAndPreservesExplicitCommand(t *testing.T) {
	plain := rayleabot.Event{Message: rayleabot.Message{PlainText: "昔涟攻略"}}
	if got := parseRequest(plain, []string{"/"}); got.Kind != requestNone {
		t.Fatalf("plain suffix request was accepted: %#v", got)
	}
	prefixed := rayleabot.Event{Message: rayleabot.Message{PlainText: "＊昔莲攻略"}}
	if got := parseRequest(prefixed, []string{"/"}); got.Kind != requestGuide || got.Query != "昔莲" {
		t.Fatalf("prefixed request = %#v", got)
	}
	explicit := rayleabot.Event{Payload: map[string]any{"command": "游戏攻略", "args": []any{"昔涟"}}}
	if got := parseRequest(explicit, []string{"/"}); got.Kind != requestGuide || got.Query != "昔涟" {
		t.Fatalf("explicit command = %#v", got)
	}
}

func TestCharacterListRenderDataGroupsCharactersAndPreservesAliases(t *testing.T) {
	data := characterListRenderData([]character{
		{Name: "昔涟", Slug: "xilian", Aliases: []string{"昔莲", ""}},
		{Name: "Archer", Slug: "archer", Aliases: []string{"阿彻"}},
		{Name: "白厄", Slug: "白厄"},
	})
	if data["total"] != 3 || !strings.Contains(data["subtitle"].(string), "3") {
		t.Fatalf("unexpected render data: %#v", data)
	}
	groups := data["groups"].([]map[string]any)
	if len(groups) != 3 || groups[0]["label"] != "A" || groups[1]["label"] != "B" || groups[2]["label"] != "X" {
		t.Fatalf("unexpected groups: %#v", groups)
	}
	xilian := groups[2]["characters"].([]map[string]any)[0]
	aliases := xilian["aliases"].([]string)
	if len(aliases) != 1 || aliases[0] != "昔莲" {
		t.Fatalf("aliases were not preserved and cleaned: %#v", aliases)
	}
}

func TestRefreshCacheExpandsDetailsSkipsFailedImagesAndReloadsCache(t *testing.T) {
	search := map[string]any{
		"retcode": 0,
		"data": map[string]any{"posts": []any{map[string]any{
			"post": map[string]any{
				"game_id": 6, "f_forum_id": 61, "post_id": "70078539",
				"subject": "昔涟攻略", "content": "昔涟角色攻略图",
				"images": []any{"https://upload-bbs.miyoushe.com/upload/search.png"},
			},
			"user": map[string]any{"nickname": "赋赋"},
		}}},
	}
	detail := map[string]any{
		"retcode": 0,
		"data": map[string]any{"post": map[string]any{
			"post": map[string]any{
				"post_id": "70078539",
				"images": []any{
					"https://upload-bbs.miyoushe.com/upload/full-1.png",
					"https://upload-bbs.miyoushe.com/upload/full-2.png",
				},
			},
		}},
	}
	actions := &fakeGuideActions{files: map[string]fakeFile{}, http: []fakeHTTP{
		{result: jsonHTTP(search)},
		{result: jsonHTTP(detail)},
		{result: imageHTTP([]byte("first image"))},
		{err: errors.New("download failed")},
	}}
	service := newGuideService(actions)
	item := resolveCharacter("昔莲")
	images := service.refreshCache(context.Background(), item)
	if len(images) != 1 || !strings.HasPrefix(images[0].File, "base64://") {
		t.Fatalf("unexpected refreshed images: %#v", images)
	}
	if len(actions.requestURLs) != 4 || !strings.Contains(actions.requestURLs[1], "getPostFull?post_id=70078539") {
		t.Fatalf("post detail was not requested: %#v", actions.requestURLs)
	}
	if _, exists := actions.files[service.indexPath(item)]; !exists {
		t.Fatalf("cache index was not written: %#v", actions.files)
	}
	cached, fromCache := service.loadCachedImages(context.Background(), item)
	if !fromCache || len(cached) != 1 || cached[0].File != images[0].File {
		t.Fatalf("cache reload = %#v, %v", cached, fromCache)
	}
}

func TestSendImagesUsesBotIdentityAndBatchesAtOneBotLimit(t *testing.T) {
	actions := &fakeGuideActions{files: map[string]fakeFile{}}
	service := newGuideService(actions)
	images := make([]guideImage, 101)
	for index := range images {
		images[index] = guideImage{File: "base64://image"}
	}
	event := &rayleabot.EventContext{
		Bot:   rayleabot.Bot{ID: "2609164374"},
		Event: rayleabot.Event{Target: rayleabot.Target{Type: "group", ID: "553855023"}},
	}
	if err := service.sendImages(context.Background(), event, character{Name: "昔涟"}, images, true); err != nil {
		t.Fatal(err)
	}
	if len(actions.forward) != 2 || len(actions.forward[0].Messages) != 100 || len(actions.forward[1].Messages) != 1 {
		t.Fatalf("unexpected forward batches: %#v", actions.forward)
	}
	data := actions.forward[0].Messages[0]["data"].(map[string]any)
	if data["uin"] != "2609164374" {
		t.Fatalf("forward node uin = %#v", data["uin"])
	}
}

type fakeHTTP struct {
	result rayleabot.ActionResult
	err    error
}

type fakeFile struct {
	text   string
	base64 string
}

type fakeGuideActions struct {
	http        []fakeHTTP
	requestURLs []string
	files       map[string]fakeFile
	forward     []rayleabot.MessageForwardSendRequest
}

func (fake *fakeGuideActions) HTTPRequest(_ context.Context, request httpRequest) (rayleabot.ActionResult, error) {
	fake.requestURLs = append(fake.requestURLs, request.URL)
	if len(fake.http) == 0 {
		return rayleabot.ActionResult{"status_code": 404}, nil
	}
	response := fake.http[0]
	fake.http = fake.http[1:]
	return response.result, response.err
}

func (fake *fakeGuideActions) FileRead(_ context.Context, path string) (rayleabot.ActionResult, error) {
	file, exists := fake.files[path]
	result := rayleabot.ActionResult{"exists": exists, "path": path}
	if file.text != "" {
		result["content_text"] = file.text
	}
	if file.base64 != "" {
		result["content_base64"] = file.base64
	}
	return result, nil
}

func (fake *fakeGuideActions) FileWriteText(_ context.Context, path, content string) (rayleabot.ActionResult, error) {
	fake.files[path] = fakeFile{text: content}
	return rayleabot.ActionResult{}, nil
}

func (fake *fakeGuideActions) FileWriteBase64(_ context.Context, path, content string) (rayleabot.ActionResult, error) {
	fake.files[path] = fakeFile{base64: content}
	return rayleabot.ActionResult{}, nil
}

func (fake *fakeGuideActions) FileList(_ context.Context, prefix string) (rayleabot.ActionResult, error) {
	paths := make([]any, 0)
	for path := range fake.files {
		if strings.HasPrefix(path, prefix) {
			paths = append(paths, path)
		}
	}
	return rayleabot.ActionResult{"paths": paths}, nil
}

func (fake *fakeGuideActions) MessageSend(context.Context, rayleabot.MessageSendRequest) (rayleabot.ActionResult, error) {
	return rayleabot.ActionResult{}, nil
}

func (fake *fakeGuideActions) MessageForwardSend(_ context.Context, request rayleabot.MessageForwardSendRequest) (rayleabot.ActionResult, error) {
	fake.forward = append(fake.forward, request)
	return rayleabot.ActionResult{}, nil
}

func (fake *fakeGuideActions) LoggerWrite(context.Context, rayleabot.LoggerWriteRequest) (rayleabot.ActionResult, error) {
	return rayleabot.ActionResult{}, nil
}

func jsonHTTP(document map[string]any) rayleabot.ActionResult {
	body, _ := json.Marshal(document)
	return rayleabot.ActionResult{"status_code": 200, "body_text": string(body)}
}

func imageHTTP(content []byte) rayleabot.ActionResult {
	return rayleabot.ActionResult{"status_code": 200, "body_base64": base64.StdEncoding.EncodeToString(content)}
}
