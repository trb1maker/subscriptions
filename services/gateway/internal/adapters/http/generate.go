package httpapi

import "net/http"

type generateRequest struct {
	Prompt string `json:"prompt"`
}

type generateResponse struct {
	Status string `json:"status"`
	Text   string `json:"text,omitempty"`
	Tokens int64  `json:"tokens"`
}

func (h handler) generate(w http.ResponseWriter, r *http.Request) {
	var body generateRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeError(w, r, err)
		return
	}

	key, err := idempotencyKey(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	result, err := h.generator.Generate(r.Context(), body.Prompt, key)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(h.log, w, r, http.StatusOK, generateResponse{
		Status: result.Status,
		Text:   result.Text,
		Tokens: result.Tokens,
	})
}
