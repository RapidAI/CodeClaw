#pragma once

#include <vector>
#include <string>
#include <cmath>

/**
 * Online speaker clustering using cosine distance.
 * Groups speaker embeddings into clusters in real-time.
 * Each cluster represents a unique speaker identity.
 */

struct SpeakerCluster {
    int id;
    std::vector<float> centroid;  // running average embedding
    int count;                     // number of embeddings in cluster
};

class OnlineClusterer {
public:
    /**
     * @param emb_dim Embedding dimension (e.g., 192 for ECAPA-TDNN)
     * @param threshold Cosine similarity threshold for same-speaker (default 0.7)
     * @param max_speakers Maximum number of speakers to track
     */
    OnlineClusterer(int emb_dim = 192, float threshold = 0.7f, int max_speakers = 32);

    /**
     * Assign a speaker embedding to a cluster.
     * Returns the speaker ID (0-based). Creates a new cluster if no match.
     * Returns -1 if max_speakers exceeded and no match found.
     */
    int Assign(const float* embedding, int dim);

    /**
     * Get the number of active speaker clusters.
     */
    int NumSpeakers() const { return (int)clusters_.size(); }

    /**
     * Reset all clusters.
     */
    void Reset();

    /**
     * Get centroid of a specific speaker cluster.
     * Returns nullptr if speaker_id is invalid.
     */
    const float* GetCentroid(int speaker_id) const;

    void SetThreshold(float t) { threshold_ = t; }
    float GetThreshold() const { return threshold_; }

private:
    int emb_dim_;
    float threshold_;
    int max_speakers_;
    std::vector<SpeakerCluster> clusters_;

    static float CosineSimilarity(const float* a, const float* b, int dim);
    static void L2Normalize(float* vec, int dim);
};
