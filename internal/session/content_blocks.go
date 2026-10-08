package session

import (
	"bytes"
	"encoding/json"
	"errors"
	agenthostcontract "github.com/36Dge/yijie-agent-host/internal/contracts"
	"io"
)

func MapTurnV2ContentBlocks(blocks []agenthostcontract.StartTurnV2ContentBlock) ([]TurnContentBlock, error) {
	mapped := make([]TurnContentBlock, 0, len(blocks))
	for _, block := range blocks {
		raw, err := block.MarshalJSON()
		if err != nil {
			return nil, err
		}
		discriminator, err := block.Discriminator()
		if err != nil {
			return nil, err
		}
		switch discriminator {
		case ContentBlockText:
			var value agenthostcontract.StartTurnV2TextBlock
			if err := strictContentBlock(raw, &value); err != nil || string(value.Type) != discriminator {
				return nil, errors.New("invalid text content block")
			}
			mapped = append(mapped, TurnContentBlock{Type: discriminator, Text: value.Text})
		case ContentBlockImage:
			var value agenthostcontract.StartTurnV2ImageBlock
			if err := strictContentBlock(raw, &value); err != nil || string(value.Type) != discriminator {
				return nil, errors.New("invalid image content block")
			}
			mapped = append(mapped, TurnContentBlock{Type: discriminator, Image: &TurnImageBlock{
				AttachmentID: value.AttachmentId.String(),
				MediaType:    string(value.MediaType),
				SizeBytes:    value.SizeBytes,
				SHA256:       value.Sha256,
				DataURL:      value.DataUrl,
			}})
		case ContentBlockFile:
			var value agenthostcontract.StartTurnV2FileBlock
			if err := strictContentBlock(raw, &value); err != nil || string(value.Type) != discriminator {
				return nil, errors.New("invalid file content block")
			}
			mapped = append(mapped, TurnContentBlock{Type: discriminator, File: &TurnFileBlock{
				AttachmentID:  value.AttachmentId.String(),
				Name:          value.Name,
				MediaType:     string(value.MediaType),
				SizeBytes:     value.SizeBytes,
				SHA256:        value.Sha256,
				ContextChunks: append([]string(nil), value.ContextChunks...),
			}})
		default:
			return nil, errors.New("unsupported content block discriminator")
		}
	}
	return mapped, nil
}

func strictContentBlock(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(value); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing content block data")
	}
	return nil
}
