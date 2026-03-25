#include "arch/online_clusterer.h"
#include <algorithm>
#include <cstring>
#include <cmath>

OnlineClusterer::OnlineClusterer(int emb_dim, float threshold, int max_speakers)
    : emb_dim_(emb_dim), threshold_(threshold), max_speakers_(max_speakers) {}

float OnlineClusterer::CosineSimilarity(const float* a, const float* b, int dim) {
    float dot = 0.0f, na = 0.0f, nb = 0.0f;
    for (int i = 0; i < dim; i++) {
        dot += a[i] * b[i];
        na += a[i] * a[i];
        nb += b[i] * b[i];
    }
    float denom = sqrtf(na) * sqrtf(nb);
    return (denom > 1e-8f) ? (dot / denom) : 0.0f;
}

void OnlineClusterer::L2Normalize(float* vec, int dim) {
    float norm = 0.0f;
    for (int i = 0; i < dim; i++) norm += vec[i] * vec[i];
    norm = sqrtf(norm);
    if (norm > 1e-8f) {
        for (int i = 0; i < dim; i++) vec[i] /= norm;
    }
}

int OnlineClusterer::Assign(const float* embedding, int dim) {
    if (dim != emb_dim_ || !embedding) return -1;

    // Normalize input
    std::vector<float> emb(embedding, embedding + dim);
    L2Normalize(emb.data(), dim);

    // Find best matching cluster
    int best_id = -1;
    float best_sim = -1.0f;

    for (size_t i = 0; i < clusters_.size(); i++) {
        float sim = CosineSimilarity(emb.data(), clusters_[i].centroid.data(), dim);
        if (sim > best_sim) {
            best_sim = sim;
            best_id = (int)i;
        }
    }

    if (best_sim >= threshold_ && best_id >= 0) {
        // Update centroid with running average
        auto& c = clusters_[best_id];
        float w = 1.0f / (c.count + 1);
        for (int i = 0; i < dim; i++) {
            c.centroid[i] = c.centroid[i] * (1.0f - w) + emb[i] * w;
        }
        L2Normalize(c.centroid.data(), dim);
        c.count++;
        return c.id;
    }

    // Create new cluster
    if ((int)clusters_.size() >= max_speakers_) return -1;

    int new_id = (int)clusters_.size();
    SpeakerCluster nc;
    nc.id = new_id;
    nc.centroid = emb;
    nc.count = 1;
    clusters_.push_back(std::move(nc));
    return new_id;
}

void OnlineClusterer::Reset() {
    clusters_.clear();
}

const float* OnlineClusterer::GetCentroid(int speaker_id) const {
    if (speaker_id < 0 || speaker_id >= (int)clusters_.size()) return nullptr;
    return clusters_[speaker_id].centroid.data();
}
