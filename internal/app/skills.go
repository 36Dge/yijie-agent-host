package app

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	agenthostcontract "github.com/36Dge/yijie-agent-host/internal/contracts"
	"github.com/36Dge/yijie-agent-host/internal/security"
	"github.com/36Dge/yijie-agent-host/internal/skills"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

var managedSkillIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
var errInvalidSkillProjection = errors.New("invalid managed Skill projection")

type SkillService interface {
	List(context.Context) (skills.Snapshot, error)
	Scan(context.Context, skills.ScanInput) (skills.Snapshot, error)
	Install(context.Context, skills.InstallInput) (skills.State, error)
	SetEnabled(context.Context, skills.EnabledInput) (skills.State, error)
	Uninstall(context.Context, skills.UninstallInput) (skills.State, error)
}

type HandlerOption func(*handlerOptions)

type handlerOptions struct {
	skillService SkillService
}

func WithSkillService(service SkillService) HandlerOption {
	return func(options *handlerOptions) {
		options.skillService = service
	}
}

type skillHandler struct {
	service  SkillService
	apiToken string
	read     bool
	manage   bool
}

func registerSkillRoutes(mux *http.ServeMux, config SkillFeatureConfig, service SkillService, apiToken string) {
	handler := &skillHandler{
		service:  service,
		apiToken: apiToken,
		read:     config.Read,
		manage:   config.Manage,
	}
	mux.Handle("GET /v1/skills", handler.authorize(handler.read, http.HandlerFunc(handler.list)))
	mux.Handle("POST /v1/skills/scan-operations", handler.authorize(handler.manage, http.HandlerFunc(handler.scan)))
	mux.Handle("POST /v1/skills/{skill_id}/install-operations", handler.authorize(handler.manage, http.HandlerFunc(handler.install)))
	mux.Handle("PUT /v1/skills/{skill_id}/enabled", handler.authorize(handler.manage, http.HandlerFunc(handler.setEnabled)))
	mux.Handle("POST /v1/skills/{skill_id}/uninstall-operations", handler.authorize(handler.manage, http.HandlerFunc(handler.uninstall)))
}

func (h *skillHandler) authorize(capability bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !security.TokenMatches(h.apiToken, r.Header.Get("Authorization")) {
			writeSkillError(w, http.StatusUnauthorized, agenthostcontract.SkillErrorResponseErrorCodeUnauthorized)
			return
		}
		if !capability {
			writeSkillError(w, http.StatusForbidden, agenthostcontract.SkillErrorResponseErrorCodeCapabilityDenied)
			return
		}
		if h.service == nil {
			writeSkillError(w, http.StatusServiceUnavailable, agenthostcontract.SkillErrorResponseErrorCodeRuntimeUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *skillHandler) list(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.service.List(r.Context())
	if err != nil {
		writeSkillServiceError(w, err)
		return
	}
	response, err := skillListResponse(snapshot)
	if err != nil {
		writeSkillInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *skillHandler) scan(w http.ResponseWriter, r *http.Request) {
	var request agenthostcontract.SkillScanRequest
	if err := decodeRequest(w, r, &request); err != nil || !request.Reason.Valid() {
		writeSkillInvalidRequest(w)
		return
	}
	snapshot, err := h.service.Scan(r.Context(), skills.ScanInput{
		OperationID: request.OperationId.String(),
		Reason:      string(request.Reason),
	})
	if err != nil {
		writeSkillServiceError(w, err)
		return
	}
	states, err := managedSkillResponses(snapshot.Skills)
	if err != nil {
		writeSkillInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, agenthostcontract.SkillScanResponse{
		CatalogRevision: snapshot.CatalogRevision,
		OperationId:     request.OperationId,
		Outcome:         agenthostcontract.SkillScanResponseOutcomeComplete,
		ScannedAt:       snapshot.ScannedAt,
		Skills:          states,
	})
}

func (h *skillHandler) install(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("skill_id")
	if !validManagedSkillID(skillID) {
		writeSkillInvalidRequest(w)
		return
	}
	var request agenthostcontract.SkillInstallRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeSkillInvalidRequest(w)
		return
	}
	state, err := h.service.Install(r.Context(), skills.InstallInput{
		OperationID:           request.OperationId.String(),
		SkillID:               skillID,
		ExpectedVersion:       request.ExpectedVersion,
		ExpectedArchiveSHA256: request.ExpectedArchiveSha256,
		CatalogRevision:       request.CatalogRevision,
	})
	if err != nil {
		writeSkillServiceError(w, err)
		return
	}
	h.writeMutation(w, request.OperationId, state)
}

func (h *skillHandler) setEnabled(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("skill_id")
	if !validManagedSkillID(skillID) {
		writeSkillInvalidRequest(w)
		return
	}
	var request agenthostcontract.SkillEnabledRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeSkillInvalidRequest(w)
		return
	}
	state, err := h.service.SetEnabled(r.Context(), skills.EnabledInput{
		OperationID: request.OperationId.String(),
		SkillID:     skillID,
		Enabled:     request.Enabled,
	})
	if err != nil {
		writeSkillServiceError(w, err)
		return
	}
	h.writeMutation(w, request.OperationId, state)
}

func (h *skillHandler) uninstall(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("skill_id")
	if !validManagedSkillID(skillID) {
		writeSkillInvalidRequest(w)
		return
	}
	var request agenthostcontract.SkillUninstallRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeSkillInvalidRequest(w)
		return
	}
	state, err := h.service.Uninstall(r.Context(), skills.UninstallInput{
		OperationID: request.OperationId.String(),
		SkillID:     skillID,
	})
	if err != nil {
		writeSkillServiceError(w, err)
		return
	}
	h.writeMutation(w, request.OperationId, state)
}

func (h *skillHandler) writeMutation(w http.ResponseWriter, operationID openapi_types.UUID, state skills.State) {
	mapped, err := managedSkillResponse(state)
	if err != nil {
		writeSkillInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, agenthostcontract.SkillMutationResponse{
		OperationId: operationID,
		Outcome:     agenthostcontract.SkillMutationResponseOutcomeComplete,
		Skill:       mapped,
	})
}

func skillListResponse(snapshot skills.Snapshot) (agenthostcontract.SkillListResponse, error) {
	states, err := managedSkillResponses(snapshot.Skills)
	if err != nil {
		return agenthostcontract.SkillListResponse{}, err
	}
	return agenthostcontract.SkillListResponse{
		CatalogRevision: snapshot.CatalogRevision,
		ScannedAt:       snapshot.ScannedAt,
		SchemaVersion:   agenthostcontract.N1,
		Skills:          states,
	}, nil
}

func managedSkillResponses(states []skills.State) ([]agenthostcontract.ManagedSkill, error) {
	result := make([]agenthostcontract.ManagedSkill, 0, len(states))
	for _, state := range states {
		mapped, err := managedSkillResponse(state)
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}

func managedSkillResponse(state skills.State) (agenthostcontract.ManagedSkill, error) {
	capability := agenthostcontract.ManagedSkillCapabilityReadiness(state.CapabilityReadiness)
	catalog := agenthostcontract.ManagedSkillCatalogStatus(state.CatalogStatus)
	failure := agenthostcontract.ManagedSkillFailureCode(state.FailureCode)
	installation := agenthostcontract.ManagedSkillInstallationStatus(state.InstallationStatus)
	maintenance := agenthostcontract.ManagedSkillMaintenanceStatus(state.MaintenanceStatus)
	if !capability.Valid() || !catalog.Valid() || !failure.Valid() || !installation.Valid() || !maintenance.Valid() {
		return agenthostcontract.ManagedSkill{}, errInvalidSkillProjection
	}
	return agenthostcontract.ManagedSkill{
		CapabilityReadiness: capability,
		CatalogStatus:       catalog,
		Enabled:             state.Enabled,
		FailureCode:         failure,
		Id:                  state.ID,
		InstallationStatus:  installation,
		MaintenanceStatus:   maintenance,
		RuntimeName:         state.RuntimeName,
		RuntimeVisible:      state.RuntimeVisible,
		Version:             state.Version,
	}, nil
}

func validManagedSkillID(value string) bool {
	return len(value) >= 3 && len(value) <= 128 && managedSkillIDPattern.MatchString(value)
}

func writeSkillServiceError(w http.ResponseWriter, err error) {
	var serviceError *skills.ServiceError
	if !errors.As(err, &serviceError) {
		writeSkillInternalError(w)
		return
	}
	code := serviceError.Code
	status := http.StatusInternalServerError
	switch code {
	case skills.CodeInvalidRequest:
		status = http.StatusBadRequest
	case skills.CodeNotFound:
		status = http.StatusNotFound
	case skills.CodeOperationConflict, skills.CodeBusy:
		status = http.StatusConflict
	case skills.CodeNotInstallable, skills.CodeBundleMissing, skills.CodeManifestInvalid,
		skills.CodeArchiveChecksum, skills.CodeArchiveUnsafe, skills.CodeArchiveTooLarge:
		status = http.StatusUnprocessableEntity
	case skills.CodeRuntimeUnavailable, skills.CodeRuntimeSyncFailed, skills.CodeCapabilityUnavailable:
		status = http.StatusServiceUnavailable
	}
	contractCode := agenthostcontract.SkillErrorResponseErrorCode(code)
	if !contractCode.Valid() {
		writeSkillInternalError(w)
		return
	}
	writeSkillError(w, status, contractCode)
}

func writeSkillInvalidRequest(w http.ResponseWriter) {
	writeSkillError(w, http.StatusBadRequest, agenthostcontract.SkillErrorResponseErrorCodeInvalidRequest)
}

func writeSkillInternalError(w http.ResponseWriter) {
	writeSkillError(w, http.StatusInternalServerError, agenthostcontract.SkillErrorResponseErrorCodeInternalError)
}

func writeSkillError(w http.ResponseWriter, status int, code agenthostcontract.SkillErrorResponseErrorCode) {
	var response agenthostcontract.SkillErrorResponse
	response.Error.Code = code
	response.Error.Message = skillErrorMessage(code)
	writeJSON(w, status, response)
}

func skillErrorMessage(code agenthostcontract.SkillErrorResponseErrorCode) string {
	switch code {
	case agenthostcontract.SkillErrorResponseErrorCodeUnauthorized:
		return "valid Agent Host bearer token required"
	case agenthostcontract.SkillErrorResponseErrorCodeCapabilityDenied:
		return "required local capability is not granted"
	case agenthostcontract.SkillErrorResponseErrorCodeInvalidRequest:
		return "request parameters are invalid"
	case agenthostcontract.SkillErrorResponseErrorCodeArchiveUnsafe:
		return "Skill archive failed safety validation"
	default:
		return "Skill operation failed"
	}
}
