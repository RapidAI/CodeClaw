#include "arch/intent_recognizer.h"
#include <cmath>
#include <algorithm>

IntentRecognizer::IntentRecognizer(float threshold, int emb_dim)
    : threshold_(threshold), emb_dim_(emb_dim) {}

float IntentRecognizer::CosineSim(const float* a, const float* b, int dim) {
    float dot = 0.0f, na = 0.0f, nb = 0.0f;
    for (int i = 0; i < dim; i++) {
        dot += a[i] * b[i];
        na += a[i] * a[i];
        nb += b[i] * b[i];
    }
    float denom = sqrtf(na) * sqrtf(nb);
    return (denom > 1e-8f) ? (dot / denom) : 0.0f;
}

void IntentRecognizer::RegisterIntent(const std::string& name,
                                       const std::vector<std::string>& phrases,
                                       const std::vector<std::vector<float>>& embeddings) {
    RegisteredIntent intent;
    intent.name = name;
    intent.trigger_phrases = phrases;
    intent.phrase_embeddings = embeddings;
    intents_.push_back(std::move(intent));
}

std::string IntentRecognizer::Match(const float* query_embedding, int dim,
                                     float* out_score) const {
    if (!query_embedding || dim <= 0) return "";

    int match_dim = std::min(dim, emb_dim_);
    float best_score = -1.0f;
    std::string best_intent;

    for (const auto& intent : intents_) {
        for (const auto& emb : intent.phrase_embeddings) {
            if ((int)emb.size() < match_dim) continue;
            float sim = CosineSim(query_embedding, emb.data(), match_dim);
            if (sim > best_score) {
                best_score = sim;
                best_intent = intent.name;
            }
        }
    }

    if (out_score) *out_score = best_score;
    return (best_score >= threshold_) ? best_intent : "";
}

void IntentRecognizer::Clear() {
    intents_.clear();
}
