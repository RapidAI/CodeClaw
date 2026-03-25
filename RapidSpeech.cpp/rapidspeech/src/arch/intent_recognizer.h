#pragma once

#include <vector>
#include <string>

/**
 * Intent recognizer using Gemma 300M embedding model.
 * Matches utterances against registered trigger phrases
 * via cosine similarity of embeddings.
 *
 * Usage:
 *   1. Register intents with trigger phrases
 *   2. For each utterance, compute embedding and match
 *
 * This is a lightweight wrapper — the actual embedding computation
 * is done by GemmaEmbeddingModel via the RapidSpeech C API.
 */

struct RegisteredIntent {
    std::string name;
    std::vector<std::string> trigger_phrases;
    std::vector<std::vector<float>> phrase_embeddings;  // pre-computed
};

class IntentRecognizer {
public:
    /**
     * @param threshold Cosine similarity threshold (default 0.7)
     * @param emb_dim Embedding dimension (default 768, can be MRL-truncated)
     */
    IntentRecognizer(float threshold = 0.7f, int emb_dim = 768);

    /**
     * Register an intent with one or more trigger phrases.
     * Embeddings must be pre-computed externally via GemmaEmbeddingModel.
     */
    void RegisterIntent(const std::string& name,
                        const std::vector<std::string>& phrases,
                        const std::vector<std::vector<float>>& embeddings);

    /**
     * Match a query embedding against registered intents.
     * Returns the best matching intent name, or empty string if below threshold.
     * out_score receives the similarity score if non-null.
     */
    std::string Match(const float* query_embedding, int dim,
                      float* out_score = nullptr) const;

    /**
     * Remove all registered intents.
     */
    void Clear();

    int NumIntents() const { return (int)intents_.size(); }
    void SetThreshold(float t) { threshold_ = t; }

private:
    float threshold_;
    int emb_dim_;
    std::vector<RegisteredIntent> intents_;

    static float CosineSim(const float* a, const float* b, int dim);
};
