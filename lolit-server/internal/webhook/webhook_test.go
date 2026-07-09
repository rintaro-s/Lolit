package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"testing"
)

const samplePCB = `(kicad_pcb (version 20221018) (generator pcbnew)
  (footprint "Resistor_SMD:R_0603_1608Metric" (layer "F.Cu")
    (fp_text reference "R1" (at 0 -1.43) (layer "F.SilkS"))
    (fp_text value "10k" (at 0 1.43) (layer "F.Fab"))
    (pad "1" smd roundrect (at -0.75 0) (size 0.9 0.95) (layers "F.Cu" "F.Paste" "F.Mask")
      (net 3 "VCC"))
    (pad "2" smd roundrect (at 0.75 0) (size 0.9 0.95) (layers "F.Cu" "F.Paste" "F.Mask")
      (net 5 "GND"))
  )
  (footprint "Capacitor_SMD:C_0603_1608Metric" (layer "F.Cu")
    (fp_text reference "C1" (at 5 -1.43) (layer "F.SilkS"))
    (fp_text value "100nF" (at 5 1.43) (layer "F.Fab"))
  )
)`

func TestParseComponentsExtractsReferenceFootprintValueAndNets(t *testing.T) {
	comps := parseComponents(samplePCB)
	if len(comps) != 2 {
		t.Fatalf("expected 2 components, got %d: %+v", len(comps), comps)
	}
	r1, ok := comps["R1"]
	if !ok {
		t.Fatalf("R1 not found in %+v", comps)
	}
	if r1.Footprint != "Resistor_SMD:R_0603_1608Metric" {
		t.Errorf("unexpected footprint: %q", r1.Footprint)
	}
	if r1.Value != "10k" {
		t.Errorf("unexpected value: %q", r1.Value)
	}
	if r1.NetSummary != "GND,VCC" {
		t.Errorf("unexpected nets: %q", r1.NetSummary)
	}

	c1, ok := comps["C1"]
	if !ok {
		t.Fatalf("C1 not found in %+v", comps)
	}
	if c1.Value != "100nF" {
		t.Errorf("unexpected value: %q", c1.Value)
	}
}

func TestParseComponentsHandlesSchematicPropertySyntax(t *testing.T) {
	sch := `(kicad_sch (version 20231120)
  (symbol (lib_id "Device:R") (at 10 20 0)
    (property "Reference" "R5" (at 0 0 0))
    (property "Value" "4k7" (at 0 0 0))
  )
)`
	comps := parseComponents(sch)
	r5, ok := comps["R5"]
	if !ok {
		t.Fatalf("R5 not found in %+v", comps)
	}
	if r5.Value != "4k7" {
		t.Errorf("unexpected value: %q", r5.Value)
	}
}

func TestDiffComponentsDetectsAddedRemovedChanged(t *testing.T) {
	old := map[string]component{
		"R1": {Ref: "R1", Value: "10k"},
		"R2": {Ref: "R2", Value: "1k"},
	}
	next := map[string]component{
		"R1": {Ref: "R1", Value: "22k"}, // changed
		"R3": {Ref: "R3", Value: "1k"},  // added
	}
	added, removed, changed := diffComponents(old, next)
	if len(added) != 1 || added[0].Ref != "R3" {
		t.Errorf("unexpected added: %+v", added)
	}
	if len(removed) != 1 || removed[0].Ref != "R2" {
		t.Errorf("unexpected removed: %+v", removed)
	}
	if len(changed) != 1 || changed[0].Ref != "R1" {
		t.Errorf("unexpected changed: %+v", changed)
	}
}

func TestHandlerVerifySignature(t *testing.T) {
	h := &Handler{WebhookSecret: ""}
	if !h.verifySignature(httptest.NewRequest("POST", "/webhook", nil), []byte("anything")) {
		t.Error("expected verification to pass when no secret is configured")
	}

	body := []byte("body")
	h.WebhookSecret = "s3cret"

	req := httptest.NewRequest("POST", "/webhook", nil)
	if h.verifySignature(req, body) {
		t.Error("expected verification to fail without a signature header")
	}

	mac := hmac.New(sha256.New, []byte(h.WebhookSecret))
	mac.Write(body)
	req = httptest.NewRequest("POST", "/webhook", nil)
	req.Header.Set("X-Gitea-Signature", hex.EncodeToString(mac.Sum(nil)))
	if !h.verifySignature(req, body) {
		t.Error("expected verification to pass with a correct signature")
	}

	req = httptest.NewRequest("POST", "/webhook", nil)
	req.Header.Set("X-Gitea-Signature", "deadbeef")
	if h.verifySignature(req, body) {
		t.Error("expected verification to fail with a wrong signature")
	}
}
