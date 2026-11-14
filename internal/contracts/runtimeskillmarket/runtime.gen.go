// Generated from canonical native Skill marketplace schemas. DO NOT EDIT.
package runtimeskillmarket

import "encoding/json"

type JSONValue = json.RawMessage
type PluginStateReadParams struct {
	Cwd              *AbsolutePathBuf `json:"cwd,omitempty"`
	MarketplaceNames []string         `json:"marketplaceNames,omitempty"`
	PluginIds        []string         `json:"pluginIds,omitempty"`
}
type AbsolutePathBuf string
type PluginStateReadResponse struct {
	States []PluginState `json:"states"`
}
type PluginCacheState string
type PluginState struct {
	CacheState        PluginCacheState `json:"cacheState"`
	CacheVersion      *string          `json:"cacheVersion,omitempty"`
	CatalogListed     bool             `json:"catalogListed"`
	EffectiveEnabled  bool             `json:"effectiveEnabled"`
	Errors            []string         `json:"errors"`
	ManifestValid     bool             `json:"manifestValid"`
	PluginId          string           `json:"pluginId"`
	Skills            []SkillSummary   `json:"skills"`
	UserConfigPresent bool             `json:"userConfigPresent"`
	UserEnabled       *bool            `json:"userEnabled,omitempty"`
}
type SkillInterface struct {
	BrandColor       *string          `json:"brandColor,omitempty"`
	DefaultPrompt    *string          `json:"defaultPrompt,omitempty"`
	DisplayName      *string          `json:"displayName,omitempty"`
	IconLarge        *AbsolutePathBuf `json:"iconLarge,omitempty"`
	IconSmall        *AbsolutePathBuf `json:"iconSmall,omitempty"`
	ShortDescription *string          `json:"shortDescription,omitempty"`
}
type SkillSummary struct {
	Description      string           `json:"description"`
	Enabled          bool             `json:"enabled"`
	Interface        *SkillInterface  `json:"interface,omitempty"`
	Name             string           `json:"name"`
	Path             *AbsolutePathBuf `json:"path,omitempty"`
	ShortDescription *string          `json:"shortDescription,omitempty"`
}
type PluginRefreshParams struct {
	Force           *bool           `json:"force,omitempty"`
	MarketplacePath AbsolutePathBuf `json:"marketplacePath"`
	PluginId        string          `json:"pluginId"`
}
type PluginRefreshResponse struct {
	Changed bool        `json:"changed"`
	State   PluginState `json:"state"`
}
type PluginReadResponse struct {
	Plugin PluginDetail `json:"plugin"`
}
type AppSummary struct {
	Category    *string `json:"category,omitempty"`
	Description *string `json:"description,omitempty"`
	Id          string  `json:"id"`
	InstallUrl  *string `json:"installUrl,omitempty"`
	Name        string  `json:"name"`
}
type AppTemplateSummary struct {
	CanonicalConnectorId *string                       `json:"canonicalConnectorId,omitempty"`
	Category             *string                       `json:"category,omitempty"`
	Description          *string                       `json:"description,omitempty"`
	LogoUrl              *string                       `json:"logoUrl,omitempty"`
	LogoUrlDark          *string                       `json:"logoUrlDark,omitempty"`
	MaterializedAppIds   []string                      `json:"materializedAppIds"`
	Name                 string                        `json:"name"`
	Reason               *AppTemplateUnavailableReason `json:"reason,omitempty"`
	TemplateId           string                        `json:"templateId"`
}
type AppTemplateUnavailableReason string
type HookEventName string
type PluginAuthPolicy string
type PluginAvailability = json.RawMessage
type PluginDetail struct {
	AppTemplates    []AppTemplateSummary `json:"appTemplates"`
	Apps            []AppSummary         `json:"apps"`
	Description     *string              `json:"description,omitempty"`
	Hooks           []PluginHookSummary  `json:"hooks"`
	MarketplaceName string               `json:"marketplaceName"`
	MarketplacePath *AbsolutePathBuf     `json:"marketplacePath,omitempty"`
	McpServers      []string             `json:"mcpServers"`
	ShareUrl        *string              `json:"shareUrl,omitempty"`
	Skills          []SkillSummary       `json:"skills"`
	Summary         PluginSummary        `json:"summary"`
}
type PluginHookSummary struct {
	EventName HookEventName `json:"eventName"`
	Key       string        `json:"key"`
}
type PluginInstallPolicy string
type PluginInstallPolicySource string
type PluginInterface struct {
	BrandColor        *string           `json:"brandColor,omitempty"`
	Capabilities      []string          `json:"capabilities"`
	Category          *string           `json:"category,omitempty"`
	ComposerIcon      *AbsolutePathBuf  `json:"composerIcon,omitempty"`
	ComposerIconUrl   *string           `json:"composerIconUrl,omitempty"`
	DefaultPrompt     *[]string         `json:"defaultPrompt,omitempty"`
	DeveloperName     *string           `json:"developerName,omitempty"`
	DisplayName       *string           `json:"displayName,omitempty"`
	Logo              *AbsolutePathBuf  `json:"logo,omitempty"`
	LogoDark          *AbsolutePathBuf  `json:"logoDark,omitempty"`
	LogoUrl           *string           `json:"logoUrl,omitempty"`
	LogoUrlDark       *string           `json:"logoUrlDark,omitempty"`
	LongDescription   *string           `json:"longDescription,omitempty"`
	PrivacyPolicyUrl  *string           `json:"privacyPolicyUrl,omitempty"`
	ScreenshotUrls    []string          `json:"screenshotUrls"`
	Screenshots       []AbsolutePathBuf `json:"screenshots"`
	ShortDescription  *string           `json:"shortDescription,omitempty"`
	TermsOfServiceUrl *string           `json:"termsOfServiceUrl,omitempty"`
	WebsiteUrl        *string           `json:"websiteUrl,omitempty"`
}
type PluginShareContext struct {
	CreatorAccountUserId *string                     `json:"creatorAccountUserId,omitempty"`
	CreatorName          *string                     `json:"creatorName,omitempty"`
	Discoverability      *PluginShareDiscoverability `json:"discoverability,omitempty"`
	RemotePluginId       string                      `json:"remotePluginId"`
	RemoteVersion        *string                     `json:"remoteVersion,omitempty"`
	SharePrincipals      *[]PluginSharePrincipal     `json:"sharePrincipals,omitempty"`
	ShareUrl             *string                     `json:"shareUrl,omitempty"`
}
type PluginShareDiscoverability string
type PluginSharePrincipal struct {
	Name          string                   `json:"name"`
	PrincipalId   string                   `json:"principalId"`
	PrincipalType PluginSharePrincipalType `json:"principalType"`
	Role          PluginSharePrincipalRole `json:"role"`
}
type PluginSharePrincipalRole string
type PluginSharePrincipalType string
type PluginSource = json.RawMessage
type PluginSummary struct {
	AuthPolicy          PluginAuthPolicy           `json:"authPolicy"`
	Availability        *json.RawMessage           `json:"availability,omitempty"`
	Enabled             bool                       `json:"enabled"`
	Id                  string                     `json:"id"`
	InstallPolicy       PluginInstallPolicy        `json:"installPolicy"`
	InstallPolicySource *PluginInstallPolicySource `json:"installPolicySource,omitempty"`
	Installed           bool                       `json:"installed"`
	Interface           *PluginInterface           `json:"interface,omitempty"`
	Keywords            []string                   `json:"keywords,omitempty"`
	LocalVersion        *string                    `json:"localVersion,omitempty"`
	Name                string                     `json:"name"`
	RemotePluginId      *string                    `json:"remotePluginId,omitempty"`
	ShareContext        *PluginShareContext        `json:"shareContext,omitempty"`
	Source              PluginSource               `json:"source"`
	Version             *string                    `json:"version,omitempty"`
}
type ListMcpServerStatusParams struct {
	Cursor   *string                `json:"cursor,omitempty"`
	Detail   *McpServerStatusDetail `json:"detail,omitempty"`
	Limit    *int64                 `json:"limit,omitempty"`
	ThreadId *string                `json:"threadId,omitempty"`
}
type McpServerStatusDetail string
type ListMcpServerStatusResponse struct {
	Data       []McpServerStatus `json:"data"`
	NextCursor *string           `json:"nextCursor,omitempty"`
}
type McpAuthStatus string
type McpServerInfo struct {
	Description *string            `json:"description,omitempty"`
	Icons       *[]json.RawMessage `json:"icons,omitempty"`
	Name        string             `json:"name"`
	Title       *string            `json:"title,omitempty"`
	Version     string             `json:"version"`
	WebsiteUrl  *string            `json:"websiteUrl,omitempty"`
}
type McpServerStatus struct {
	AuthStatus        McpAuthStatus      `json:"authStatus"`
	Name              string             `json:"name"`
	ResourceTemplates []ResourceTemplate `json:"resourceTemplates"`
	Resources         []Resource         `json:"resources"`
	ServerInfo        *McpServerInfo     `json:"serverInfo,omitempty"`
	Tools             json.RawMessage    `json:"tools"`
}
type Resource struct {
	Meta        *json.RawMessage   `json:"_meta,omitempty"`
	Annotations *json.RawMessage   `json:"annotations,omitempty"`
	Description *string            `json:"description,omitempty"`
	Icons       *[]json.RawMessage `json:"icons,omitempty"`
	MimeType    *string            `json:"mimeType,omitempty"`
	Name        string             `json:"name"`
	Size        *int64             `json:"size,omitempty"`
	Title       *string            `json:"title,omitempty"`
	Uri         string             `json:"uri"`
}
type ResourceTemplate struct {
	Annotations *json.RawMessage `json:"annotations,omitempty"`
	Description *string          `json:"description,omitempty"`
	MimeType    *string          `json:"mimeType,omitempty"`
	Name        string           `json:"name"`
	Title       *string          `json:"title,omitempty"`
	UriTemplate string           `json:"uriTemplate"`
}
type Tool struct {
	Meta         *json.RawMessage   `json:"_meta,omitempty"`
	Annotations  *json.RawMessage   `json:"annotations,omitempty"`
	Description  *string            `json:"description,omitempty"`
	Icons        *[]json.RawMessage `json:"icons,omitempty"`
	InputSchema  json.RawMessage    `json:"inputSchema"`
	Name         string             `json:"name"`
	OutputSchema *json.RawMessage   `json:"outputSchema,omitempty"`
	Title        *string            `json:"title,omitempty"`
}

const RuntimeManifestSHA256 = "eed98a79cd1fb1687faf295df32d2087504f28704d3cb9775afe902d7e6399aa"
const RuntimeBinarySHA256 = "e03bdbd2b4167022ee85c2df86dacff1a0a5c082321a9aa19875b29136a96cf5"
const RuntimeBinarySize int64 = 358227400
const RuntimeSchemaTreeSHA256 = "97489c7264dfd3b827210c6a4132716a1983b2ad89e383a7a80316ec0d8c258c"
const RuntimeSchemaFileCount = 275
const RuntimePatchManifest = "[{\"path\":\".yijie/patches/0001-feat-126-filter-persistent-diagnostics.patch\",\"sha256\":\"6b337a02caf064c6819fab5c7367a485004c85cce0d42acb06fa6d5003e599a0\"},{\"path\":\".yijie/patches/0002-feat-136-unified-exec-pre-emitter-command-lifecycle.patch\",\"sha256\":\"43de168e1443f4b9ca60d7f61e3de2daf20e1cfea14d2e196d28ba417bf3e06d\"},{\"path\":\".yijie/patches/input-only/0003-input-only-execution.patch\",\"sha256\":\"c340b6fc17419bcdc1ff45eebe3da40e5fde740c029102eb7a188f6b14eb41d8\"},{\"path\":\".yijie/patches/chat-models/0004-feat-156-upstream-tool-choice.patch\",\"sha256\":\"828f913fead29da08c015d73ef6c7102a8adf9afd3811ab5469891ec5efc2abf\"},{\"path\":\".yijie/patches/chat-models/0005-feat-156-terminal-tool-arguments.patch\",\"sha256\":\"f1d6689f44056c252530ad940bb253e8a0ace60e810b88857440699ba908917e\"},{\"path\":\".yijie/patches/chat-models/0006-native-web-search-sources.patch\",\"sha256\":\"8fdb9d2f7d7708a603a8d08906a4d3cd6ce098ca9165471f95cb0816ed6718ee\"},{\"path\":\".yijie/patches/chat-models/0007-feat-164-kimi-k31-pro.patch\",\"sha256\":\"c666c066ec1235d016e7479805242d049def542b9f8c684970ac8e5b4cee8ae6\"},{\"path\":\".yijie/patches/chat-models/0008-feat-164-kimi-responses-include.patch\",\"sha256\":\"c3dc2b58e6ae9d1a2335bc4937defbbaf9da99848575d1e554fc579ef9aec475\"},{\"path\":\".yijie/patches/chat-models/0009-feat-164-native-failure-terminals.patch\",\"sha256\":\"ae74c11488a31644e9b052f1864fd4a76ecdc05e9019206efd2e7d82f8595886\"},{\"path\":\".yijie/patches/chat-models/0010-feat-164-sanitized-persistence-metrics.patch\",\"sha256\":\"bcffb5337101c7d2032acaa0dc633cd92f2d12110eff0be10a79884f38eb85a7\"},{\"path\":\".yijie/patches/chat-models/0011-feat-164-graceful-state-close.patch\",\"sha256\":\"b5054bce23e12f5638cddbf54f4936a8116d62f6ba62208efa58132580c95e83\"},{\"path\":\".yijie/patches/chat-models/0012-web-retrieval-execution-guard.patch\",\"sha256\":\"235fc906fe30868fab8135909701f6bdaf21f422eb90959ea10de4bd58c1ff09\"},{\"path\":\".yijie/patches/chat-models/0013-answer-completion.patch\",\"sha256\":\"36b9b9b245971493120b3f6d334ef7daa15b310b94b11de1440d28a4c2c1ce73\"},{\"path\":\".yijie/patches/chat-models/0014-execution-progress.patch\",\"sha256\":\"453ca48015787976b063ebc89c56965a7cdfa82fb980c98f03c57216cbd0940b\"},{\"path\":\".yijie/patches/chat-models/0015-public-progress-completion.patch\",\"sha256\":\"41e8fd2e1a9497d5424e50307aa31b7114c632a8e59c7c7adb2acb5cd0a12ba4\"},{\"path\":\".yijie/patches/chat-models/0016-native-public-messages.patch\",\"sha256\":\"2351e6a7941f3cf24a4f1efe2ec40b6f43254d320c3ae09469cbf18e64e86945\"},{\"path\":\".yijie/patches/chat-models/0017-native-public-message-validation.patch\",\"sha256\":\"392a4c4141cabedcad148e2ce9b0133a64287152cefb16c576295409da9bb84b\"},{\"path\":\".yijie/patches/chat-models/0018-native-public-message-spelling.patch\",\"sha256\":\"91deb2b8fda2c6e2d0f64a835f5fcf30930715c44e63943a763419a39276dff2\"},{\"path\":\".yijie/patches/chat-models/0019-host-image-generation.patch\",\"sha256\":\"5c202edda265bdec4a8af87ed4d1b7101ab4e4fbb3e48f1fa59bb09947272f6a\"},{\"path\":\".yijie/patches/chat-models/0020-native-tool-integration.patch\",\"sha256\":\"0dabd01b0e3d9195eb5b949999a075c3bb5183a4a1586bb2eb1ede5c5b0920a2\"},{\"path\":\".yijie/patches/chat-models/0021-native-tool-policy-audit.patch\",\"sha256\":\"c210fd77202411c8f6b4827296284e6a62e19005176af11780fcb64ce94b46a8\"},{\"path\":\".yijie/patches/chat-models/0022-user-input-context-tools.patch\",\"sha256\":\"a4ea07269228bb428e89236e1882e0bf48294e0c35a3848c0795c05ec7c8877f\"},{\"path\":\".yijie/patches/chat-models/0023-tool-search-function-transport.patch\",\"sha256\":\"34ba57c1cd38af909979a4e74df37ba63e02e61a2b9e67ef2dbf99518ecb5bd6\"},{\"path\":\".yijie/patches/chat-models/0024-cooperative-exec-shutdown.patch\",\"sha256\":\"09fa17f39f7e1b1d4969ee1ee644ba6539b7fe3d3892b9416151601aadf8dab9\"},{\"path\":\".yijie/patches/chat-models/0025-native-skill-marketplace.patch\",\"sha256\":\"0b93c9ac1a236199b6fb7b6bd781b2ed0d29e9329c5e237e5ace90aacc7bbc85\"}]"
