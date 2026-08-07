package casc

import "testing"

func TestAdaptiveResumeChunkSize(t *testing.T) {
	tests := []struct {
		size int64
		want int64
	}{
		{1 << 20, 1 << 20},
		{16 << 20, 4 << 20},
		{64 << 20, 4 << 20},
		{128 << 20, 8 << 20},
	}
	for _, tt := range tests {
		if got := adaptiveResumeChunkSize(tt.size); got != tt.want {
			t.Fatalf("size=%d chunk=%d, want %d", tt.size, got, tt.want)
		}
	}
}

func TestBuildResumeJobsMergesOnlyAdjacentMissingChunks(t *testing.T) {
	state := resumeState{Complete: []bool{true, false, false, false, true, false}}
	jobs := buildResumeJobs(state, 6<<20, 1<<20, 4<<20)
	if len(jobs) != 2 {
		t.Fatalf("jobs=%d, want 2: %#v", len(jobs), jobs)
	}
	if jobs[0].start != 1<<20 || jobs[0].end != 4<<20-1 || len(jobs[0].indexes) != 3 {
		t.Fatalf("merged job=%#v", jobs[0])
	}
	if jobs[1].start != 5<<20 || jobs[1].end != 6<<20-1 || len(jobs[1].indexes) != 1 {
		t.Fatalf("separate job=%#v", jobs[1])
	}
}

func TestBuildResumeJobsKeepsSegmentsWhenMergeDisabled(t *testing.T) {
	state := resumeState{Complete: []bool{false, false, false}}
	jobs := buildResumeJobs(state, 3<<20, 1<<20, 0)
	if len(jobs) != 3 {
		t.Fatalf("jobs=%d, want 3: %#v", len(jobs), jobs)
	}
}
