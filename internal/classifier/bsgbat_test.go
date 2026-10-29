package classifier

import (
	"bufio"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/conf/conftest"
	"github.com/tphakala/birdnet-go/internal/detection"
)

const (
	bsgBatTestDataEnv           = "BSGBAT_TESTDATA_DIR"
	bsgBatReferenceSegments     = 8
	bsgBatReferenceFixtureCount = 9
	bsgBatReferenceStride       = 250 * bsgBatHopSize
	bsgBatFileEdgePadding       = bsgBatFFTSize / 2
	bsgBatParityTolerance       = 0.05
)

func TestBSGBatLabelsAndWiring(t *testing.T) {
	assert.True(t, isBatFamily(RegistryIDBSGBat))
	info, ok := ModelRegistry[RegistryIDBSGBat]
	require.True(t, ok)
	assert.Equal(t, "BSG-BAT", info.DetectionName)
	assert.Equal(t, bsgBatRawSampleRate, info.Spec.SampleRate)
	assert.Equal(t, bsgBatRawSampleRate, info.Spec.EffectiveSampleRate())
	entry, ok := GetCatalogEntry("bsg-bat-eu")
	require.True(t, ok)
	assert.Equal(t, RegistryIDBSGBat, entry.RegistryID)
	assert.Equal(t, "bat", string(detection.ResolveModelType("BSG-BAT", "0.21")))
}

func TestBSGBatReferenceParity(t *testing.T) {
	testDataDir := requireBSGBatTestData(t)
	model := newBSGBatTestModel(t, filepath.Join(testDataDir, "bsgbat_v0.21_r1.onnx"), filepath.Join(testDataDir, "bsgbat_labels.txt"))
	for example := 1; example <= 3; example++ {
		wavPath := filepath.Join(testDataDir, "ref", "example"+strconv.Itoa(example)+".wav")
		samples := readBSGBatWAV(t, wavPath)
		wantRows := readBSGBatLogits(t, wavPath+".logits")
		require.Len(t, wantRows, bsgBatReferenceSegments)
		maxDifference := 0.0
		for segment := range bsgBatReferenceSegments {
			window := bsgBatReferenceWindow(samples, segment)
			got, err := model.runLogits(window)
			require.NoError(t, err)
			for class, want := range wantRows[segment] {
				difference := math.Abs(float64(got[class] - want))
				maxDifference = math.Max(maxDifference, difference)
			}
		}
		assert.LessOrEqual(t, maxDifference, bsgBatParityTolerance, "example%d.wav max abs diff: %.6f", example, maxDifference)
		if !t.Failed() {
			t.Logf("example%d.wav max abs diff: %.6f", example, maxDifference)
		}
	}
}

func TestBSGBatFunctional(t *testing.T) {
	original := conf.Setting()
	settings := *original
	settings.Bat.Threshold = 0
	conftest.SetTestSettings(&settings)
	t.Cleanup(func() { conftest.SetTestSettings(original) })
	testDataDir := requireBSGBatTestData(t)
	model := newBSGBatTestModel(t, filepath.Join(testDataDir, "bsgbat_v0.21_r3.onnx"), filepath.Join(testDataDir, "bsgbat_labels.txt"))
	tests := []struct {
		example int
		want    string
	}{{1, "Barbastella barbastellus"}, {2, "Pipistrellus pipistrellus"}, {3, "Myotis daubentonii"}}
	for _, test := range tests {
		samples := readBSGBatWAV(t, filepath.Join(testDataDir, "ref", "example"+strconv.Itoa(test.example)+".wav"))
		maxLogits := make([]float32, len(model.labels))
		for i := range maxLogits {
			maxLogits[i] = -math.MaxFloat32
		}
		for segment := range bsgBatReferenceSegments {
			window := bsgBatReferenceWindow(samples, segment)
			full, err := model.runLogits(window)
			require.NoError(t, err)
			full = slices.Clone(full)
			results, err := model.Predict(t.Context(), [][]float32{window[:bsgBatRawSampleRate]})
			require.NoError(t, err)
			require.NotEmpty(t, results)
			logits := model.output.GetData()
			maxDifference := 0.0
			bestFull := 0
			for class := range model.labels {
				if full[class] > full[bestFull] {
					bestFull = class
				}
			}
			for class := range full {
				maxDifference = math.Max(maxDifference, math.Abs(float64(full[class]-logits[class])))
			}
			assert.Equal(t, model.labels[bestFull], results[0].Species, "example%d segment%d", test.example, segment)
			t.Logf("example%d segment%d padded-vs-full max logit diff: %.6f", test.example, segment, maxDifference)

			for class := range maxLogits {
				maxLogits[class] = max(maxLogits[class], logits[class])
			}
		}
		best := 0
		for i := 1; i < len(maxLogits); i++ {
			if maxLogits[i] > maxLogits[best] {
				best = i
			}
		}
		assert.True(t, strings.HasPrefix(model.labels[best], test.want+"_"), "example%d top label: %s", test.example, model.labels[best])
	}
}

func requireBSGBatTestData(t *testing.T) string {
	t.Helper()
	directory := os.Getenv(bsgBatTestDataEnv)
	if directory == "" {
		t.Skip("BSGBAT_TESTDATA_DIR is not configured")
	}
	// Reference parity needs both model revisions and the original WAV/logit
	// fixtures. Model-only downloads can still run the candidate-floor test.
	files := make([]string, 0, bsgBatReferenceFixtureCount)
	files = append(files, "bsgbat_v0.21_r1.onnx", "bsgbat_v0.21_r3.onnx", "bsgbat_labels.txt")
	for example := 1; example <= 3; example++ {
		wav := filepath.Join("ref", "example"+strconv.Itoa(example)+".wav")
		files = append(files, wav, wav+".logits")
	}
	for _, file := range files {
		if _, err := os.Stat(filepath.Join(directory, file)); err != nil {
			t.Skipf("BSG-BAT reference fixture unavailable: %s", file)
		}
	}
	return directory
}

func newBSGBatTestModel(t *testing.T, modelPath, labelPath string) *BSGBat {
	t.Helper()
	model, err := NewBSGBat(&BSGBatModelConfig{ModelPath: modelPath, LabelPath: labelPath, ONNXRuntimePath: os.Getenv("ONNX_RUNTIME_PATH"), Threads: 1})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, model.Close()) })
	return model
}

func bsgBatReferenceWindow(samples []float32, segment int) []float32 {
	window := make([]float32, bsgBatInputSamples)
	start := segment*bsgBatReferenceStride - bsgBatFileEdgePadding
	copyStart := max(start, 0)
	copyEnd := min(start+bsgBatInputSamples, len(samples))
	if copyStart < copyEnd {
		copy(window[copyStart-start:], samples[copyStart:copyEnd])
	}
	return window
}

func readBSGBatWAV(t *testing.T, path string) []float32 {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(data), 12)
	require.Equal(t, "RIFF", string(data[:4]))
	require.Equal(t, "WAVE", string(data[8:12]))
	for offset := 12; offset+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start, end := offset+8, offset+8+size
		require.LessOrEqual(t, end, len(data))
		if string(data[offset:offset+4]) == "data" {
			require.Zero(t, size%2)
			samples := make([]float32, size/2)
			for i := range samples {
				samples[i] = float32(int16(binary.LittleEndian.Uint16(data[start+i*2:]))) / 32768
			}
			return samples
		}
		offset = end + size%2
	}
	require.FailNow(t, "WAV data chunk not found", path)
	return nil
}

func readBSGBatLogits(t *testing.T, path string) [][]float32 {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer func() { assert.NoError(t, file.Close()) }()
	rows := make([][]float32, 0, bsgBatReferenceSegments)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		row := make([]float32, len(fields))
		for i, field := range fields {
			value, parseErr := strconv.ParseFloat(field, 32)
			require.NoError(t, parseErr)
			row[i] = float32(value)
		}
		require.Len(t, row, bsgBatOutputClasses)
		rows = append(rows, row)
	}
	require.NoError(t, scanner.Err())
	return rows
}

func TestBSGBatNormalizeSilenceIsFinite(t *testing.T) {
	t.Parallel()
	d, dErr := newBSGBatDSP()
	require.NoError(t, dErr)
	out, pErr := d.preprocess(make([]float32, bsgBatInputSamples))
	require.NoError(t, pErr)
	for i, v := range out {
		require.False(t, math.IsNaN(float64(v)) || math.IsInf(float64(v), 0), "index %d", i)
	}
}
