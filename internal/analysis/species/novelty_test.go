package species

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore"
)

func testNoveltyTracker(windowDays int) *SpeciesTracker {
	return NewTrackerFromSettings(nil, &conf.SpeciesTrackingSettings{
		Enabled:              true,
		NewSpeciesWindowDays: windowDays,
		YearlyTracking: conf.YearlyTrackingSettings{
			Enabled: false,
		},
		SeasonalTracking: conf.SeasonalTrackingSettings{
			Enabled: false,
		},
	})
}

func TestCheckAndUpdateSpeciesWithNovelty_FirstEverEpisode(t *testing.T) {
	t.Parallel()

	tracker := testNoveltyTracker(7)
	detectionTime := time.Date(2026, 5, 23, 8, 0, 0, 0, time.UTC)

	isNew, daysSinceFirst, novelty := tracker.CheckAndUpdateSpeciesWithNovelty("Setophaga castanea", detectionTime)

	assert.True(t, isNew)
	assert.Equal(t, 0, daysSinceFirst)
	assert.True(t, novelty.NoveltyEpisodeActive)
	assert.Equal(t, inactiveNoveltyValue, novelty.DaysSinceLastSeen)
	assert.Equal(t, firstEverNoveltyEpisodeDays, novelty.NoveltyEpisodeDays)
	assert.Equal(t, detectionTime, novelty.NoveltyEpisodeStart)
}

func TestCheckAndUpdateSpeciesWithNovelty_ReturnAfterAbsenceEpisode(t *testing.T) {
	t.Parallel()

	tracker := testNoveltyTracker(7)
	firstTime := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	returnTime := firstTime.AddDate(0, 0, 12)

	_, _, _ = tracker.CheckAndUpdateSpeciesWithNovelty("Setophaga castanea", firstTime)
	_, daysSinceFirst, novelty := tracker.CheckAndUpdateSpeciesWithNovelty("Setophaga castanea", returnTime)

	assert.Equal(t, 12, daysSinceFirst)
	assert.True(t, novelty.NoveltyEpisodeActive)
	assert.Equal(t, 12, novelty.DaysSinceLastSeen)
	assert.Equal(t, 12, novelty.NoveltyEpisodeDays)
	assert.Equal(t, returnTime, novelty.NoveltyEpisodeStart)
}

func TestCheckAndUpdateSpeciesWithNovelty_EpisodePersistsForWindow(t *testing.T) {
	t.Parallel()

	tracker := testNoveltyTracker(7)
	firstTime := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	returnTime := firstTime.AddDate(0, 0, 12)
	nextDay := returnTime.AddDate(0, 0, 1)

	_, _, _ = tracker.CheckAndUpdateSpeciesWithNovelty("Setophaga castanea", firstTime)
	_, _, _ = tracker.CheckAndUpdateSpeciesWithNovelty("Setophaga castanea", returnTime)
	_, _, novelty := tracker.CheckAndUpdateSpeciesWithNovelty("Setophaga castanea", nextDay)

	assert.True(t, novelty.NoveltyEpisodeActive)
	assert.Equal(t, 1, novelty.DaysSinceLastSeen)
	assert.Equal(t, 12, novelty.NoveltyEpisodeDays)
	assert.Equal(t, returnTime, novelty.NoveltyEpisodeStart)
}

func TestCheckAndUpdateSpeciesWithNovelty_NoEpisodeForSameDayDetection(t *testing.T) {
	t.Parallel()

	tracker := testNoveltyTracker(7)
	detectionTime := time.Date(2026, 5, 23, 8, 0, 0, 0, time.UTC)
	const scientificName = "Setophaga castanea"

	tracker.speciesFirstSeen[scientificName] = detectionTime.AddDate(0, 0, -30)
	tracker.speciesLastSeen[scientificName] = detectionTime

	_, _, novelty := tracker.CheckAndUpdateSpeciesWithNovelty(scientificName, detectionTime.Add(2*time.Hour))

	assert.False(t, novelty.NoveltyEpisodeActive)
	assert.Equal(t, 0, novelty.DaysSinceLastSeen)
	assert.Equal(t, inactiveNoveltyValue, novelty.NoveltyEpisodeDays)
}

func testInfrequentTracker(windowDays, infrequentDays int) *SpeciesTracker {
	return NewTrackerFromSettings(nil, &conf.SpeciesTrackingSettings{
		Enabled:              true,
		NewSpeciesWindowDays: windowDays,
		YearlyTracking:       conf.YearlyTrackingSettings{Enabled: false},
		SeasonalTracking:     conf.SeasonalTrackingSettings{Enabled: false},
		InfrequentTracking: conf.InfrequentTrackingSettings{
			Enabled: true,
			Days:    infrequentDays,
		},
	})
}

func TestSpeciesStatus_InfrequentSurvivesSameDayRedetection(t *testing.T) {
	t.Parallel()

	const scientificName = "Setophaga castanea"
	// Absence (45d) must exceed windowDays (14d) so the first-ever episode
	// expires and a fresh return episode forms carrying the real 45-day gap,
	// which clears the 30-day infrequent threshold.
	tracker := testInfrequentTracker(14, 30)

	firstTime := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	returnTime := firstTime.AddDate(0, 0, 45)
	nextDay := returnTime.AddDate(0, 0, 1)

	_, _, _ = tracker.CheckAndUpdateSpeciesWithNovelty(scientificName, firstTime)
	_, _, _ = tracker.CheckAndUpdateSpeciesWithNovelty(scientificName, returnTime)

	// On return, the 45-day absence flags the species as infrequent.
	assert.True(t, tracker.GetSpeciesStatus(scientificName, returnTime).IsInfrequent,
		"species should be infrequent on the day it returns after a 45-day absence")

	// A next-day re-detection collapses DaysSinceLastSeen to 1, but the episode
	// still represents the original 45-day absence, so the flag must persist.
	_, _, _ = tracker.CheckAndUpdateSpeciesWithNovelty(scientificName, nextDay)
	status := tracker.GetSpeciesStatus(scientificName, nextDay)
	assert.Equal(t, 1, status.DaysSinceLastSeen)
	assert.True(t, status.IsInfrequent,
		"infrequent flag must survive a same-/next-day re-detection within the episode")
}

func TestSpeciesStatus_InfrequentExcludesFirstEverAndShortAbsence(t *testing.T) {
	t.Parallel()

	t.Run("first-ever detection is not infrequent", func(t *testing.T) {
		t.Parallel()
		const scientificName = "Setophaga castanea"
		tracker := testInfrequentTracker(14, 30)
		firstTime := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)

		_, _, _ = tracker.CheckAndUpdateSpeciesWithNovelty(scientificName, firstTime)

		// NoveltyEpisodeDays is the firstEverNoveltyEpisodeDays sentinel (~100y);
		// it must not be mistaken for a long absence.
		assert.False(t, tracker.GetSpeciesStatus(scientificName, firstTime).IsInfrequent)
	})

	t.Run("absence below threshold is not infrequent", func(t *testing.T) {
		t.Parallel()
		const scientificName = "Setophaga castanea"
		tracker := testInfrequentTracker(14, 30)
		firstTime := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
		// 20-day absence: exceeds windowDays (14) so a fresh return episode forms,
		// but is below the 30-day infrequent threshold.
		returnTime := firstTime.AddDate(0, 0, 20)

		_, _, _ = tracker.CheckAndUpdateSpeciesWithNovelty(scientificName, firstTime)
		_, _, _ = tracker.CheckAndUpdateSpeciesWithNovelty(scientificName, returnTime)

		require.Equal(t, 20, tracker.GetSpeciesStatus(scientificName, returnTime).DaysSinceLastSeen)
		assert.False(t, tracker.GetSpeciesStatus(scientificName, returnTime).IsInfrequent)
	})
}

func TestLoadNoveltyEpisodesFromDatabase_RestoresActiveEpisode(t *testing.T) {
	t.Parallel()

	const scientificName = "Setophaga castanea"
	now := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)
	runStart := trackerDateOnly(now)
	previousDate := runStart.AddDate(0, 0, -12)

	ds := &noveltyHistoryDatastore{
		lifetime: []datastore.NewSpeciesData{
			{
				ScientificName: scientificName,
				CommonName:     "Bay-breasted Warbler",
				FirstSeenDate:  previousDate.Format(time.DateOnly),
				LastSeenDate:   runStart.Format(time.DateOnly),
			},
		},
		detectionDates: []datastore.SpeciesDetectionDate{
			{ScientificName: scientificName, Date: runStart.Format(time.DateOnly)},
		},
		previousDates: map[string]string{
			scientificName + "|" + runStart.Format(time.DateOnly): previousDate.Format(time.DateOnly),
		},
	}

	tracker := testNoveltyTracker(7)
	tracker.ds = ds
	require.NoError(t, tracker.loadLifetimeDataFromDatabase(t.Context(), now))
	require.NoError(t, tracker.loadNoveltyEpisodesFromDatabase(t.Context(), now))

	_, _, novelty := tracker.CheckAndUpdateSpeciesWithNovelty(scientificName, now.Add(2*time.Hour))

	assert.True(t, novelty.NoveltyEpisodeActive)
	assert.Equal(t, 0, novelty.DaysSinceLastSeen)
	assert.Equal(t, 12, novelty.NoveltyEpisodeDays)
	assert.Equal(t, runStart.Format(time.DateOnly), novelty.NoveltyEpisodeStart.Format(time.DateOnly))
}

func TestLoadNoveltyEpisodesFromDatabase(t *testing.T) {
	t.Parallel()

	const scientificName = "Setophaga castanea"
	now := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)
	runStart := trackerDateOnly(now)
	previousDate := runStart.AddDate(0, 0, -12)

	tests := []struct {
		name                   string
		ds                     *noveltyHistoryDatastore
		wantDaysSinceLastSeen  int
		wantNoveltyEpisodeDays int
	}{
		{
			// The restored absence gap must match the value the live path records
			// at episode creation (12), not days-since-latest-detection (0).
			name: "restores absence gap",
			ds: &noveltyHistoryDatastore{
				lifetime: []datastore.NewSpeciesData{
					{
						ScientificName: scientificName,
						CommonName:     "Bay-breasted Warbler",
						FirstSeenDate:  previousDate.Format(time.DateOnly),
						LastSeenDate:   runStart.Format(time.DateOnly),
					},
				},
				detectionDates: []datastore.SpeciesDetectionDate{
					{ScientificName: scientificName, Date: runStart.Format(time.DateOnly)},
				},
				previousDates: map[string]string{
					scientificName + "|" + runStart.Format(time.DateOnly): previousDate.Format(time.DateOnly),
				},
			},
			wantDaysSinceLastSeen:  12,
			wantNoveltyEpisodeDays: 12,
		},
		{
			// A first-ever species has no prior sighting, so the restored episode
			// must use the inactive sentinel for DaysSinceLastSeen rather than the
			// multi-decade firstEver sentinel, which the API would surface as a gap.
			name: "first ever has no absence gap",
			ds: &noveltyHistoryDatastore{
				lifetime: []datastore.NewSpeciesData{
					{
						ScientificName: scientificName,
						CommonName:     "Bay-breasted Warbler",
						FirstSeenDate:  runStart.Format(time.DateOnly),
						LastSeenDate:   runStart.Format(time.DateOnly),
					},
				},
				detectionDates: []datastore.SpeciesDetectionDate{
					{ScientificName: scientificName, Date: runStart.Format(time.DateOnly)},
				},
			},
			wantDaysSinceLastSeen:  inactiveNoveltyValue,
			wantNoveltyEpisodeDays: firstEverNoveltyEpisodeDays,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tracker := testNoveltyTracker(7)
			tracker.ds = tt.ds
			require.NoError(t, tracker.loadLifetimeDataFromDatabase(t.Context(), now))
			require.NoError(t, tracker.loadNoveltyEpisodesFromDatabase(t.Context(), now))

			// Inspect the restored episode directly, before any new detection
			// re-runs the live path and overwrites the restored value.
			episode, ok := tracker.noveltyEpisodes[scientificName]
			require.True(t, ok)
			assert.True(t, episode.NoveltyEpisodeActive)
			assert.Equal(t, tt.wantDaysSinceLastSeen, episode.DaysSinceLastSeen)
			assert.Equal(t, tt.wantNoveltyEpisodeDays, episode.NoveltyEpisodeDays)
		})
	}
}

type noveltyHistoryDatastore struct {
	lifetime       []datastore.NewSpeciesData
	detectionDates []datastore.SpeciesDetectionDate
	previousDates  map[string]string
}

func (d *noveltyHistoryDatastore) GetNewSpeciesDetections(context.Context, string, string, int, int) ([]datastore.NewSpeciesData, error) {
	return d.lifetime, nil
}

func (d *noveltyHistoryDatastore) GetSpeciesFirstDetectionInPeriod(context.Context, string, string, int, int) ([]datastore.NewSpeciesData, error) {
	return nil, nil
}

func (d *noveltyHistoryDatastore) GetActiveNotificationHistory(context.Context, time.Time) ([]datastore.NotificationHistory, error) {
	return nil, nil
}

func (d *noveltyHistoryDatastore) SaveNotificationHistory(context.Context, *datastore.NotificationHistory) error {
	return nil
}

func (d *noveltyHistoryDatastore) DeleteExpiredNotificationHistory(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (d *noveltyHistoryDatastore) GetSpeciesDetectionDatesInPeriod(context.Context, string, string, int, int) ([]datastore.SpeciesDetectionDate, error) {
	return d.detectionDates, nil
}

func (d *noveltyHistoryDatastore) GetSpeciesLastDetectionDateBefore(_ context.Context, scientificName, beforeDate string) (string, error) {
	return d.previousDates[scientificName+"|"+beforeDate], nil
}
