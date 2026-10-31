package classifier

// BatCandidateFloor keeps low-confidence candidates visible to the processor,
// which applies the current per-model or shared bat threshold.
const BatCandidateFloor float32 = 0.1
