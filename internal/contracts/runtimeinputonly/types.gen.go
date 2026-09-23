// Code generated from the native input-only stable schema. DO NOT EDIT.
package runtimeinputonly

type AbsolutePathBuf = string
type InputOnlyExecutionPolicy struct {
	Cwd                          AbsolutePathBuf       `json:"cwd"`
	ExtensionContributorsEnabled bool                  `json:"extensionContributorsEnabled"`
	FileReadRoots                []AbsolutePathBuf     `json:"fileReadRoots"`
	FileWriteRoots               []AbsolutePathBuf     `json:"fileWriteRoots"`
	InstructionSources           []LegacyAppPathString `json:"instructionSources"`
	NetworkAccess                bool                  `json:"networkAccess"`
	ToolNames                    []string              `json:"toolNames"`
	Version                      uint32                `json:"version"`
}
type LegacyAppPathString = string
type ThreadInputOnlyPolicyReadParams struct {
	ThreadId string `json:"threadId"`
}
type ThreadInputOnlyPolicyReadResponse struct {
	Policy   *InputOnlyExecutionPolicy `json:"policy,omitempty"`
	ThreadId string                    `json:"threadId"`
}

const Method = "thread/inputOnlyPolicy/read"
const RuntimeBinarySHA256 = "bd7d26205e2d735dcac0f35fc089a7b30a5c18a54586b94a2c7f624f2f5b7672"
const RuntimeManifestSHA256 = "14d4073de87be137cf39f770c091b8ab62a7c34933bc49dc50f12b4ddd3a494a"
const RuntimeBinarySize int64 = 356184808
const RuntimeSchemaFileCount = 269
const RuntimeSchemaTreeSHA256 = "34d353815dc8d800cb432a876d5b43350511f63b91ad9766f861e8bc8290cc92"
const RuntimePatchManifest = "[{\"path\":\".yijie/patches/0001-feat-126-filter-persistent-diagnostics.patch\",\"sha256\":\"6b337a02caf064c6819fab5c7367a485004c85cce0d42acb06fa6d5003e599a0\"},{\"path\":\".yijie/patches/0002-feat-136-unified-exec-pre-emitter-command-lifecycle.patch\",\"sha256\":\"43de168e1443f4b9ca60d7f61e3de2daf20e1cfea14d2e196d28ba417bf3e06d\"},{\"path\":\".yijie/patches/input-only/0003-input-only-execution.patch\",\"sha256\":\"c340b6fc17419bcdc1ff45eebe3da40e5fde740c029102eb7a188f6b14eb41d8\"}]"
