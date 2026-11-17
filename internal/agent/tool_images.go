package agent

import (
	"strings"

	"superbot/internal/llm"
	"superbot/internal/tools"
)

// Tools can return pictures (a desktop screenshot). OpenAI-style tool
// messages carry text only, so the pictures of one step follow the step's
// tool results as a user message, which every provider accepts. Only the
// latest screenshot stays in the context: an older one is replaced by a
// note, so a long desktop session does not resend every frame.

// toolImagesMarker starts the text of the message that carries tool images.
const toolImagesMarker = "【系统】以下是工具返回的截图"

const toolImagesDropped = "【系统】（较早的工具截图已省略，最新截图见后文）"

type toolImage struct {
	tool  string
	image llm.Image
}

// collectToolImages remembers the pictures a tool result carries.
func collectToolImages(images []toolImage, toolName string, result tools.Result) []toolImage {
	for _, image := range result.Images {
		if len(image.Data) > 0 && image.MIMEType != "" {
			images = append(images, toolImage{tool: toolName, image: image})
		}
	}
	return images
}

// maxStepImages bounds the pictures one step adds.
const maxStepImages = 2

// appendToolImages adds the step's pictures for a model that accepts
// images. A text-only model gets nothing: the tool results already say what
// is on the screen.
func appendToolImages(messages []llm.Message, images []toolImage, vision bool) []llm.Message {
	if !vision || len(images) == 0 {
		return messages
	}
	for i := range messages {
		if isToolImagesMessage(messages[i]) {
			messages[i] = llm.TextMessage(llm.RoleUser, toolImagesDropped)
		}
	}
	if len(images) > maxStepImages {
		images = images[len(images)-maxStepImages:]
	}
	names := make([]string, 0, len(images))
	parts := []llm.Part{{Type: llm.PartText}}
	for _, item := range images {
		names = append(names, item.tool)
		image := item.image
		parts = append(parts, llm.Part{Type: llm.PartImage, Image: &image})
	}
	parts[0].Text = toolImagesMarker + "（" + strings.Join(names, "、") + "）。图中的坐标就是屏幕像素坐标。"
	return append(messages, llm.Message{Role: llm.RoleUser, Parts: parts})
}

func isToolImagesMessage(message llm.Message) bool {
	return message.Role == llm.RoleUser && len(message.Parts) > 0 &&
		message.Parts[0].Type == llm.PartText && strings.HasPrefix(message.Parts[0].Text, toolImagesMarker)
}
