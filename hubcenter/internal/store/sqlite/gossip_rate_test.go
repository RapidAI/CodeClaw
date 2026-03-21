package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func newTestGossipRepo(t *testing.T) *gossipRepo {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "gossip-rate-test.db")
	provider, err := NewProvider(Config{
		DSN:               dbPath,
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	return &gossipRepo{db: provider.Write, readDB: provider.Read, batch: provider.batch}
}

func seedPost(t *testing.T, repo *gossipRepo, postID string) {
	t.Helper()
	ctx := context.Background()
	err := repo.CreatePost(ctx, &store.GossipPost{
		ID:        postID,
		MachineID: "seed-machine",
		Nickname:  "MaClaw-seed",
		Content:   "test post",
		Category:  "owner",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed post: %v", err)
	}
}

// 5.1 — First rating succeeds
func TestRateComment_FirstRatingSucceeds(t *testing.T) {
	repo := newTestGossipRepo(t)
	seedPost(t, repo, "post-1")

	comment := &store.GossipComment{
		ID:        "c-1",
		PostID:    "post-1",
		MachineID: "machine-A",
		Nickname:  "MaClaw-aaa",
		Content:   "Rated 5/5",
		Rating:    5,
		CreatedAt: time.Now().UTC(),
	}
	if err := repo.RateComment(context.Background(), comment); err != nil {
		t.Fatalf("first rating should succeed: %v", err)
	}

	// Verify score updated
	post, err := repo.GetPost(context.Background(), "post-1")
	if err != nil {
		t.Fatalf("get post: %v", err)
	}
	if post.Score != 5 || post.Votes != 1 {
		t.Fatalf("expected score=5 votes=1, got score=%d votes=%d", post.Score, post.Votes)
	}
}

// 5.2 — Duplicate rating returns ErrAlreadyRated
func TestRateComment_DuplicateReturnsErrAlreadyRated(t *testing.T) {
	repo := newTestGossipRepo(t)
	seedPost(t, repo, "post-1")

	comment := &store.GossipComment{
		ID: "c-1", PostID: "post-1", MachineID: "machine-A",
		Nickname: "MaClaw-aaa", Content: "Rated 4/5", Rating: 4, CreatedAt: time.Now().UTC(),
	}
	if err := repo.RateComment(context.Background(), comment); err != nil {
		t.Fatalf("first rating: %v", err)
	}

	comment2 := &store.GossipComment{
		ID: "c-2", PostID: "post-1", MachineID: "machine-A",
		Nickname: "MaClaw-aaa", Content: "Rated 2/5", Rating: 2, CreatedAt: time.Now().UTC(),
	}
	err := repo.RateComment(context.Background(), comment2)
	if !errors.Is(err, store.ErrAlreadyRated) {
		t.Fatalf("expected ErrAlreadyRated, got %v", err)
	}

	// Verify only one rating exists and score unchanged
	post, _ := repo.GetPost(context.Background(), "post-1")
	if post.Score != 4 || post.Votes != 1 {
		t.Fatalf("expected score=4 votes=1, got score=%d votes=%d", post.Score, post.Votes)
	}
}

// 5.4 — Pure comment (rating=0) still works via CreateComment
func TestCreateComment_PureCommentUnaffected(t *testing.T) {
	repo := newTestGossipRepo(t)
	seedPost(t, repo, "post-1")

	comment := &store.GossipComment{
		ID: "c-1", PostID: "post-1", MachineID: "machine-A",
		Nickname: "MaClaw-aaa", Content: "just a comment", Rating: 0, CreatedAt: time.Now().UTC(),
	}
	if err := repo.CreateComment(context.Background(), comment); err != nil {
		t.Fatalf("pure comment should succeed: %v", err)
	}

	// Can still rate after pure comment
	rate := &store.GossipComment{
		ID: "c-2", PostID: "post-1", MachineID: "machine-A",
		Nickname: "MaClaw-aaa", Content: "Rated 3/5", Rating: 3, CreatedAt: time.Now().UTC(),
	}
	if err := repo.RateComment(context.Background(), rate); err != nil {
		t.Fatalf("rating after pure comment should succeed: %v", err)
	}
}

// 5.5 — Different machine_id can rate the same post
func TestRateComment_DifferentMachinesSucceed(t *testing.T) {
	repo := newTestGossipRepo(t)
	seedPost(t, repo, "post-1")

	for i := 0; i < 5; i++ {
		c := &store.GossipComment{
			ID: fmt.Sprintf("c-%d", i), PostID: "post-1", MachineID: fmt.Sprintf("machine-%d", i),
			Nickname: "MaClaw-xxx", Content: "Rated 3/5", Rating: 3, CreatedAt: time.Now().UTC(),
		}
		if err := repo.RateComment(context.Background(), c); err != nil {
			t.Fatalf("machine-%d rating failed: %v", i, err)
		}
	}

	post, _ := repo.GetPost(context.Background(), "post-1")
	if post.Score != 15 || post.Votes != 5 {
		t.Fatalf("expected score=15 votes=5, got score=%d votes=%d", post.Score, post.Votes)
	}
}

// 5.6 — Concurrent rating: only one succeeds for same (post_id, machine_id)
func TestRateComment_ConcurrentSameMachine(t *testing.T) {
	repo := newTestGossipRepo(t)
	seedPost(t, repo, "post-1")

	const goroutines = 10
	var wg sync.WaitGroup
	results := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c := &store.GossipComment{
				ID: fmt.Sprintf("c-concurrent-%d", idx), PostID: "post-1", MachineID: "same-machine",
				Nickname: "MaClaw-xxx", Content: "Rated 4/5", Rating: 4, CreatedAt: time.Now().UTC(),
			}
			results <- repo.RateComment(context.Background(), c)
		}(i)
	}
	wg.Wait()
	close(results)

	var successes, alreadyRated, otherErrors int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, store.ErrAlreadyRated):
			alreadyRated++
		default:
			otherErrors++
			t.Logf("unexpected error: %v", err)
		}
	}

	if successes != 1 {
		t.Fatalf("expected exactly 1 success, got %d (alreadyRated=%d, otherErrors=%d)", successes, alreadyRated, otherErrors)
	}

	// Verify only one rating record
	post, _ := repo.GetPost(context.Background(), "post-1")
	if post.Votes != 1 {
		t.Fatalf("expected 1 vote after concurrent rating, got %d", post.Votes)
	}
}
