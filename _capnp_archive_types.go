//go:build never

package zap

import "encoding/json"

// ToolData represents a tool definition.
type ToolData struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Schema      json.RawMessage   `json:"schema,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ToolResultData represents the result of a tool call.
type ToolResultData struct {
	ID       string            `json:"id"`
	Content  json.RawMessage   `json:"content,omitempty"`
	Error    string            `json:"error,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ResourceData represents a resource definition.
type ResourceData struct {
	URI         string            `json:"uri"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	MimeType    string            `json:"mimeType"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ResourceContentData represents resource content.
type ResourceContentData struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
	Blob     []byte `json:"blob,omitempty"`
}

// PromptData represents a prompt definition.
type PromptData struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Arguments   []PromptArgument   `json:"arguments,omitempty"`
}

// PromptArgument represents a prompt argument.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// PromptMessageData represents a message in a prompt.
type PromptMessageData struct {
	Role    string      `json:"role"`
	Content ContentData `json:"content"`
}

// ContentData represents message content.
type ContentData struct {
	Text     string               `json:"text,omitempty"`
	Image    *ImageContentData    `json:"image,omitempty"`
	Resource *ResourceContentData `json:"resource,omitempty"`
}

// ImageContentData represents image content.
type ImageContentData struct {
	Data     []byte `json:"data"`
	MimeType string `json:"mimeType"`
}

// ServerInfoData represents server information.
type ServerInfoData struct {
	Name         string           `json:"name"`
	Version      string           `json:"version"`
	Capabilities CapabilitiesData `json:"capabilities"`
}

// CapabilitiesData represents server capabilities.
type CapabilitiesData struct {
	Tools     bool `json:"tools"`
	Resources bool `json:"resources"`
	Prompts   bool `json:"prompts"`
	Logging   bool `json:"logging"`
}

// Helper functions to convert Cap'n Proto types to Go data types

func toolToData(tool Tool) (*ToolData, error) {
	name, _ := tool.Name()
	desc, _ := tool.Description()
	schema, _ := tool.Schema()

	annotations, err := tool.Annotations()
	if err != nil {
		return nil, err
	}

	return &ToolData{
		Name:        name,
		Description: desc,
		Schema:      schema,
		Annotations: metadataToMap(annotations),
	}, nil
}

func toolResultToData(result ToolResult) (*ToolResultData, error) {
	id, _ := result.Id()
	content, _ := result.Content()
	errMsg, _ := result.Error()
	meta, _ := result.Metadata()

	return &ToolResultData{
		ID:       id,
		Content:  content,
		Error:    errMsg,
		Metadata: metadataToMap(meta),
	}, nil
}

func resourceToData(resource Resource) (*ResourceData, error) {
	uri, _ := resource.Uri()
	name, _ := resource.Name()
	desc, _ := resource.Description()
	mime, _ := resource.MimeType()
	annotations, _ := resource.Annotations()

	return &ResourceData{
		URI:         uri,
		Name:        name,
		Description: desc,
		MimeType:    mime,
		Annotations: metadataToMap(annotations),
	}, nil
}

func resourceContentToData(content ResourceContent) (*ResourceContentData, error) {
	uri, _ := content.Uri()
	mime, _ := content.MimeType()

	data := &ResourceContentData{
		URI:      uri,
		MimeType: mime,
	}

	switch content.Content().Which() {
	case ResourceContent_content_Which_text:
		text, _ := content.Content().Text()
		data.Text = text
	case ResourceContent_content_Which_blob:
		blob, _ := content.Content().Blob()
		data.Blob = blob
	}

	return data, nil
}

func promptToData(prompt Prompt) (*PromptData, error) {
	name, _ := prompt.Name()
	desc, _ := prompt.Description()
	args, _ := prompt.Arguments()

	var arguments []PromptArgument
	for i := 0; i < args.Len(); i++ {
		arg := args.At(i)
		argName, _ := arg.Name()
		argDesc, _ := arg.Description()
		arguments = append(arguments, PromptArgument{
			Name:        argName,
			Description: argDesc,
			Required:    arg.Required(),
		})
	}

	return &PromptData{
		Name:        name,
		Description: desc,
		Arguments:   arguments,
	}, nil
}

func promptMessageToData(msg PromptMessage) (*PromptMessageData, error) {
	role := ""
	switch msg.Role() {
	case PromptMessage_Role_user:
		role = "user"
	case PromptMessage_Role_assistant:
		role = "assistant"
	case PromptMessage_Role_system:
		role = "system"
	}

	content, _ := msg.Content()
	contentData := ContentData{}

	switch content.Which() {
	case PromptMessage_Content_Which_text:
		text, _ := content.Text()
		contentData.Text = text
	case PromptMessage_Content_Which_image:
		img, _ := content.Image()
		imgData, _ := img.Data()
		imgMime, _ := img.MimeType()
		contentData.Image = &ImageContentData{
			Data:     imgData,
			MimeType: imgMime,
		}
	case PromptMessage_Content_Which_resource:
		res, _ := content.Resource()
		resData, _ := resourceContentToData(res)
		contentData.Resource = resData
	}

	return &PromptMessageData{
		Role:    role,
		Content: contentData,
	}, nil
}

func serverInfoToData(info ServerInfo) (*ServerInfoData, error) {
	name, _ := info.Name()
	version, _ := info.Version()
	caps, _ := info.Capabilities()

	return &ServerInfoData{
		Name:    name,
		Version: version,
		Capabilities: CapabilitiesData{
			Tools:     caps.Tools(),
			Resources: caps.Resources(),
			Prompts:   caps.Prompts(),
			Logging:   caps.Logging(),
		},
	}, nil
}

func metadataToMap(meta Metadata) map[string]string {
	result := make(map[string]string)
	entries, err := meta.Entries()
	if err != nil {
		return result
	}

	for i := 0; i < entries.Len(); i++ {
		entry := entries.At(i)
		key, _ := entry.Key()
		value, _ := entry.Value()
		result[key] = value
	}

	return result
}
