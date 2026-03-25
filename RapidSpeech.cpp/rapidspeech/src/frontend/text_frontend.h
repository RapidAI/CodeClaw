#pragma once

#include <string>
#include <vector>
#include <unordered_map>
#include <cstdint>

/**
 * Text Frontend for TTS: converts text to phoneme ID sequences.
 *
 * Supports:
 *   - Chinese: simplified pinyin-based G2P with tone marks
 *   - English: CMU dictionary subset + rule-based fallback
 *
 * The phoneme vocabulary is shared with the OpenVoice2/MeloTTS model.
 */
class TextFrontend {
public:
  TextFrontend();
  ~TextFrontend() = default;

  /// Load phoneme vocabulary from a file or use built-in defaults.
  /// vocab_path can be nullptr to use built-in vocab.
  bool Init(const char* vocab_path = nullptr);

  /// Convert text to phoneme IDs for the given language.
  /// language: "zh", "en" (default: "zh")
  /// Returns phoneme ID sequence suitable for the TTS text encoder.
  std::vector<int32_t> TextToPhonemeIds(const std::string& text,
                                         const std::string& language = "zh") const;

  /// Get vocabulary size (for model validation)
  int VocabSize() const { return static_cast<int>(phoneme_to_id_.size()); }

private:
  // Phoneme → ID mapping
  std::unordered_map<std::string, int32_t> phoneme_to_id_;

  // Chinese: character → pinyin mapping (simplified)
  std::unordered_map<uint32_t, std::string> char_to_pinyin_;

  // English: word → phoneme sequence (CMU dict subset)
  std::unordered_map<std::string, std::vector<std::string>> en_lexicon_;

  // Internal methods
  std::vector<std::string> ChineseG2P(const std::string& text) const;
  std::vector<std::string> EnglishG2P(const std::string& text) const;
  std::vector<std::string> EnglishRuleFallback(const std::string& word) const;

  void InitBuiltinVocab();
  void InitChinesePinyin();
  void InitEnglishLexicon();
};
