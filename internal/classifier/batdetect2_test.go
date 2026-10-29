package classifier

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/audiocore/resample"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/conf/conftest"
	"github.com/tphakala/birdnet-go/internal/detection"
)

const batDetect2TestDataEnv = "BATDETECT2_TESTDATA_DIR"
const batDetect2ExamplesEnv = "BATDETECT2_EXAMPLES_DIR"

func TestBatDetect2DSPDimensions(t *testing.T) {
	dsp, err := newBatDetect2DSP(batDetect2ExpectedSamples)
	require.NoError(t, err)
	_, width := dsp.preprocess(make([]float32, batDetect2ExpectedSamples))
	assert.Equal(t, 563, width)
}

func TestBatDetect2PeriodicHann(t *testing.T) {
	dsp, err := newBatDetect2DSP(batDetect2ExpectedSamples)
	require.NoError(t, err)
	w := dsp.plan.Window()
	assert.InDelta(t, 0, w[0], 1e-12)
	assert.InDelta(t, 1, w[len(w)/2], 1e-6)
}

func TestBatDetect2NMS(t *testing.T) {
	det := make([]float32, 100)
	det[2*10+2], det[2*10+3], det[8*10+8] = 0.8, 0.5, 0.9
	assert.ElementsMatch(t, []batDetect2Peak{{0.8, 2, 2}, {0.9, 8, 8}}, batDetect2NMS(det, 10, 10, 10))
}

func TestBatDetect2LabelsAndWiring(t *testing.T) {
	require.Len(t, batDetect2CodeLabels, 17)
	seen := make(map[string]bool, 17)
	for _, scientificName := range batDetect2CodeLabels {
		assert.NotEmpty(t, batCommonNames[scientificName])
		assert.False(t, seen[scientificName])
		seen[scientificName] = true
	}
	assert.True(t, isBatFamily(RegistryIDBatDetect2))
	info, ok := ModelRegistry[RegistryIDBatDetect2]
	require.True(t, ok)
	assert.Equal(t, "BatDetect2", info.DetectionName)
	entry, ok := GetCatalogEntry("batdetect2-uk")
	require.True(t, ok)
	assert.Equal(t, RegistryIDBatDetect2, entry.RegistryID)
	assert.Equal(t, "bat", string(detection.ResolveModelType("BatDetect2", "2.0.0b3")))
}

func TestBatDetect2Functional(t *testing.T) {
	testDataDir := os.Getenv(batDetect2TestDataEnv)
	if testDataDir == "" {
		t.Skip("BATDETECT2_TESTDATA_DIR is not configured")
	}
	modelPath := filepath.Join(testDataDir, "batdetect2-uk-same.onnx")
	labelPath := filepath.Join(testDataDir, "labels.json")
	if _, err := os.Stat(modelPath); err != nil {
		t.Skip("BatDetect2 ONNX test model is unavailable")
	}
	runtimePath := os.Getenv("ONNX_RUNTIME_PATH")
	model, err := NewBatDetect2(&BatDetect2ModelConfig{ModelPath: modelPath, LabelPath: labelPath, ONNXRuntimePath: runtimePath, Threads: 1})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, model.Close()) })
	_, err = model.Predict(t.Context(), [][]float32{make([]float32, 144000)})
	assert.NoError(t, err)
}

func TestBatDetect2RealAudio(t *testing.T) {
	examplesDir := os.Getenv(batDetect2ExamplesEnv)
	if examplesDir == "" {
		t.Skip("BATDETECT2_EXAMPLES_DIR is not configured")
	}
	testDataDir := os.Getenv(batDetect2TestDataEnv)
	if testDataDir == "" {
		t.Skip("BATDETECT2_TESTDATA_DIR is not configured")
	}
	for _, path := range []string{filepath.Join(testDataDir, "batdetect2-uk-same.onnx"), filepath.Join(testDataDir, "labels.json")} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("required file unavailable: %s", path)
		}
	}
	model, err := NewBatDetect2(&BatDetect2ModelConfig{ModelPath: filepath.Join(testDataDir, "batdetect2-uk-same.onnx"), LabelPath: filepath.Join(testDataDir, "labels.json"), ONNXRuntimePath: os.Getenv("ONNX_RUNTIME_PATH"), Threads: 1})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, model.Close()) })
	original := conf.Setting()
	settings := *original
	settings.Bat.Threshold = 0
	conftest.SetTestSettings(&settings)
	t.Cleanup(func() { conftest.SetTestSettings(original) })
	wants := map[string]string{"MYOMYS": "Myotis mystacinus", "EPTSER": "Eptesicus serotinus", "RHIFER": "Rhinolophus ferrumequinum"}
	for code, want := range wants {
		t.Run(code, func(t *testing.T) {
			matches, globErr := filepath.Glob(filepath.Join(examplesDir, "*"+code+"*.wav"))
			require.NoError(t, globErr)
			if len(matches) == 0 {
				t.Skip("reference audio unavailable")
			}
			require.Len(t, matches, 1)
			data, readErr := os.ReadFile(matches[0])
			require.NoError(t, readErr)
			require.GreaterOrEqual(t, len(data), 44)
			rate := int(binary.LittleEndian.Uint32(data[24:28]))
			offset := strings.Index(string(data[12:]), "data")
			require.GreaterOrEqual(t, offset, 0)
			offset += 20
			size := int(binary.LittleEndian.Uint32(data[offset-4 : offset]))
			pcm, rsErr := resample.ResampleBytes(data[offset:offset+size], rate, batDetect2SampleRate)
			require.NoError(t, rsErr)
			samples := make([]float32, batDetect2ExpectedSamples)
			for i := 0; i < len(pcm)/2 && i < len(samples); i++ {
				samples[i] = float32(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / 32768
			}
			results, predictErr := model.Predict(t.Context(), [][]float32{samples})
			require.NoError(t, predictErr)
			require.NotEmpty(t, results)
			assert.True(t, strings.HasPrefix(results[0].Species, want+"_"))
			t.Logf("%s confidence: %.3f", code, results[0].Confidence)
		})
	}
}

func TestBatDetect2NMSAcrossSeam(t *testing.T) {
	const width = 563
	det := make([]float32, width)
	det[255], det[256] = .8, .9
	assert.Equal(t, []batDetect2Peak{{.9, 0, 256}}, batDetect2NMS(det, 1, width, width))
	det[255] = .9
	assert.Len(t, batDetect2NMS(det, 1, width, width), 2)
}
