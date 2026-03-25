#include "frontend/text_frontend.h"
#include "utils/rs_log.h"

#include <algorithm>
#include <cctype>
#include <cstring>
#include <fstream>
#include <sstream>

// =====================================================================
// UTF-8 helpers
// =====================================================================

static uint32_t utf8_decode_char(const char*& p, const char* end) {
  if (p >= end) return 0;
  uint8_t c = static_cast<uint8_t>(*p);
  uint32_t cp = 0;
  int extra = 0;

  if (c < 0x80) { cp = c; extra = 0; }
  else if ((c & 0xE0) == 0xC0) { cp = c & 0x1F; extra = 1; }
  else if ((c & 0xF0) == 0xE0) { cp = c & 0x0F; extra = 2; }
  else if ((c & 0xF8) == 0xF0) { cp = c & 0x07; extra = 3; }
  else { ++p; return 0xFFFD; }  // invalid

  ++p;
  for (int i = 0; i < extra && p < end; i++) {
    cp = (cp << 6) | (static_cast<uint8_t>(*p) & 0x3F);
    ++p;
  }
  return cp;
}

static bool is_chinese_char(uint32_t cp) {
  // CJK Unified Ideographs: U+4E00 - U+9FFF
  // CJK Extension A: U+3400 - U+4DBF
  return (cp >= 0x4E00 && cp <= 0x9FFF) ||
         (cp >= 0x3400 && cp <= 0x4DBF);
}

static std::string to_lower(const std::string& s) {
  std::string r = s;
  std::transform(r.begin(), r.end(), r.begin(), ::tolower);
  return r;
}

// =====================================================================
// TextFrontend implementation
// =====================================================================

TextFrontend::TextFrontend() {}

bool TextFrontend::Init(const char* vocab_path) {
  if (vocab_path) {
    // Load vocab from file: one line per phoneme, ID = line number
    std::ifstream f(vocab_path);
    if (!f.is_open()) {
      RS_LOG_ERR("Failed to open vocab file: %s", vocab_path);
      return false;
    }
    std::string line;
    int32_t id = 0;
    while (std::getline(f, line)) {
      if (!line.empty()) {
        phoneme_to_id_[line] = id++;
      }
    }
    RS_LOG_INFO("Loaded %d phonemes from %s", id, vocab_path);
  } else {
    InitBuiltinVocab();
  }

  InitChinesePinyin();
  InitEnglishLexicon();
  return true;
}

void TextFrontend::InitBuiltinVocab() {
  // MeloTTS/OpenVoice2 phoneme set (simplified)
  // Special tokens
  const char* phonemes[] = {
    "<pad>", "<sos>", "<eos>", "<unk>", " ", ".",  ",",  "!",  "?",
    // Vowels (English ARPAbet + Chinese finals)
    "AA", "AE", "AH", "AO", "AW", "AY", "EH", "ER", "EY",
    "IH", "IY", "OW", "OY", "UH", "UW",
    // Consonants
    "B", "CH", "D", "DH", "F", "G", "HH", "JH", "K", "L",
    "M", "N", "NG", "P", "R", "S", "SH", "T", "TH", "V",
    "W", "Y", "Z", "ZH",
    // Chinese initials (pinyin)
    "b", "p", "m", "f", "d", "t", "n", "l", "g", "k", "h",
    "j", "q", "x", "zh", "ch", "sh", "r", "z", "c", "s",
    // Chinese finals (pinyin)
    "a", "o", "e", "i", "u", "v",
    "ai", "ei", "ao", "ou", "an", "en", "ang", "eng", "ong",
    "ia", "ie", "iao", "iu", "ian", "in", "iang", "ing", "iong",
    "ua", "uo", "uai", "ui", "uan", "un", "uang",
    "ve", "van", "vn",
    // Tones (1-5, 5=neutral)
    "1", "2", "3", "4", "5",
    // Silence/breath
    "sil", "sp",
  };

  int32_t id = 0;
  for (const char* ph : phonemes) {
    phoneme_to_id_[ph] = id++;
  }
  RS_LOG_INFO("Built-in vocab: %d phonemes", id);
}

void TextFrontend::InitChinesePinyin() {
  // Simplified pinyin table for common Chinese characters
  // In production, this would be a much larger table loaded from file
  // Format: Unicode codepoint -> pinyin with tone number
  static const struct { uint32_t cp; const char* py; } table[] = {
    {0x4F60, "ni3"}, {0x597D, "hao3"}, {0x4E16, "shi4"}, {0x754C, "jie4"},
    {0x6211, "wo3"}, {0x4EEC, "men5"}, {0x662F, "shi4"}, {0x4E2D, "zhong1"},
    {0x56FD, "guo2"}, {0x4EBA, "ren2"}, {0x5927, "da4"},  {0x5B66, "xue2"},
    {0x751F, "sheng1"}, {0x65F6, "shi2"}, {0x5019, "hou4"}, {0x5E74, "nian2"},
    {0x6765, "lai2"}, {0x5C31, "jiu4"}, {0x8FD9, "zhe4"}, {0x90A3, "na4"},
    {0x4E0D, "bu4"}, {0x4E86, "le5"},  {0x7684, "de5"},  {0x5730, "di4"},
    {0x5F97, "de2"}, {0x548C, "he2"},  {0x5728, "zai4"}, {0x6709, "you3"},
    {0x4E3A, "wei4"}, {0x4E0A, "shang4"}, {0x4E0B, "xia4"}, {0x4E00, "yi1"},
    {0x4E8C, "er4"}, {0x4E09, "san1"}, {0x56DB, "si4"},  {0x4E94, "wu3"},
    {0x516D, "liu4"}, {0x4E03, "qi1"},  {0x516B, "ba1"},  {0x4E5D, "jiu3"},
    {0x5341, "shi2"}, {0x767E, "bai3"}, {0x5343, "qian1"}, {0x4E07, "wan4"},
    {0x8BF4, "shuo1"}, {0x8981, "yao4"}, {0x80FD, "neng2"}, {0x4F1A, "hui4"},
    {0x53EF, "ke3"}, {0x4EE5, "yi3"},  {0x6240, "suo3"}, {0x5C31, "jiu4"},
    {0x8FD8, "hai2"}, {0x5F88, "hen3"}, {0x591A, "duo1"}, {0x5C11, "shao3"},
    {0x8BA9, "rang4"}, {0x7ED9, "gei3"}, {0x53BB, "qu4"},  {0x8FC7, "guo4"},
    {0x505A, "zuo4"}, {0x770B, "kan4"}, {0x60F3, "xiang3"}, {0x77E5, "zhi1"},
    {0x9053, "dao4"}, {0x5929, "tian1"}, {0x6C34, "shui3"}, {0x706B, "huo3"},
    {0x5C71, "shan1"}, {0x6728, "mu4"}, {0x91D1, "jin1"}, {0x571F, "tu3"},
    {0x6708, "yue4"}, {0x65E5, "ri4"}, {0x661F, "xing1"}, {0x98CE, "feng1"},
    {0x96E8, "yu3"},  {0x96EA, "xue3"}, {0x82B1, "hua1"}, {0x8349, "cao3"},
    {0x9E1F, "niao3"}, {0x9C7C, "yu2"}, {0x9A6C, "ma3"}, {0x725B, "niu2"},
    {0x7F8A, "yang2"}, {0x732B, "mao1"}, {0x72D7, "gou3"}, {0x7236, "fu4"},
    {0x6BCD, "mu3"}, {0x5144, "xiong1"}, {0x59D0, "jie3"}, {0x5F1F, "di4"},
    {0x59B9, "mei4"}, {0x513F, "er2"}, {0x5973, "nv3"}, {0x7537, "nan2"},
    {0x8001, "lao3"}, {0x5C0F, "xiao3"}, {0x65B0, "xin1"}, {0x957F, "chang2"},
    {0x77ED, "duan3"}, {0x9AD8, "gao1"}, {0x4F4E, "di1"}, {0x5FEB, "kuai4"},
    {0x6162, "man4"}, {0x5DE6, "zuo3"}, {0x53F3, "you4"}, {0x524D, "qian2"},
    {0x540E, "hou4"}, {0x91CC, "li3"}, {0x5916, "wai4"}, {0x5F00, "kai1"},
    {0x5173, "guan1"}, {0x95E8, "men2"}, {0x7A97, "chuang1"},
    // ... in production, load from a pinyin table file
  };

  for (const auto& entry : table) {
    char_to_pinyin_[entry.cp] = entry.py;
  }
}

void TextFrontend::InitEnglishLexicon() {
  // Minimal CMU dictionary subset for common words
  // In production, load the full CMUdict from file
  static const struct { const char* word; const char* phonemes; } dict[] = {
    {"the",    "DH AH"},
    {"a",      "AH"},
    {"is",     "IH Z"},
    {"are",    "AA R"},
    {"was",    "W AA Z"},
    {"were",   "W ER"},
    {"be",     "B IY"},
    {"have",   "HH AE V"},
    {"has",    "HH AE Z"},
    {"had",    "HH AE D"},
    {"do",     "D UW"},
    {"does",   "D AH Z"},
    {"did",    "D IH D"},
    {"will",   "W IH L"},
    {"would",  "W UH D"},
    {"can",    "K AE N"},
    {"could",  "K UH D"},
    {"should", "SH UH D"},
    {"may",    "M EY"},
    {"might",  "M AY T"},
    {"must",   "M AH S T"},
    {"shall",  "SH AE L"},
    {"i",      "AY"},
    {"you",    "Y UW"},
    {"he",     "HH IY"},
    {"she",    "SH IY"},
    {"it",     "IH T"},
    {"we",     "W IY"},
    {"they",   "DH EY"},
    {"this",   "DH IH S"},
    {"that",   "DH AE T"},
    {"hello",  "HH AH L OW"},
    {"world",  "W ER L D"},
    {"good",   "G UH D"},
    {"morning","M AO R N IH NG"},
    {"thank",  "TH AE NG K"},
    {"please", "P L IY Z"},
    {"yes",    "Y EH S"},
    {"no",     "N OW"},
    {"not",    "N AA T"},
    {"and",    "AE N D"},
    {"or",     "AO R"},
    {"but",    "B AH T"},
    {"in",     "IH N"},
    {"on",     "AA N"},
    {"at",     "AE T"},
    {"to",     "T UW"},
    {"for",    "F AO R"},
    {"with",   "W IH DH"},
    {"from",   "F R AH M"},
    {"of",     "AH V"},
  };

  for (const auto& entry : dict) {
    std::vector<std::string> phs;
    std::istringstream iss(entry.phonemes);
    std::string ph;
    while (iss >> ph) phs.push_back(ph);
    en_lexicon_[entry.word] = phs;
  }
}

// =====================================================================
// G2P methods
// =====================================================================

std::vector<std::string> TextFrontend::ChineseG2P(const std::string& text) const {
  std::vector<std::string> phonemes;
  const char* p = text.c_str();
  const char* end = p + text.size();

  while (p < end) {
    uint32_t cp = utf8_decode_char(p, end);
    if (cp == 0) break;

    if (is_chinese_char(cp)) {
      auto it = char_to_pinyin_.find(cp);
      if (it != char_to_pinyin_.end()) {
        // Parse pinyin: e.g. "ni3" -> initial "n" + final "i" + tone "3"
        const std::string& py = it->second;
        // Split into initial + final + tone
        // Simplified: just emit the whole pinyin as a phoneme
        // The model's text encoder handles pinyin tokens
        phonemes.push_back(py);
      } else {
        phonemes.push_back("<unk>");
      }
    } else if (cp == ' ' || cp == '\t' || cp == '\n') {
      phonemes.push_back("sp");
    } else if (cp == '.' || cp == 0x3002) {  // period or Chinese period
      phonemes.push_back(".");
      phonemes.push_back("sil");
    } else if (cp == ',' || cp == 0xFF0C) {
      phonemes.push_back(",");
      phonemes.push_back("sp");
    } else if (cp == '!' || cp == 0xFF01) {
      phonemes.push_back("!");
    } else if (cp == '?' || cp == 0xFF1F) {
      phonemes.push_back("?");
    } else if (cp < 0x80 && std::isalpha(static_cast<char>(cp))) {
      // English word embedded in Chinese text — collect full word
      std::string word;
      word += static_cast<char>(cp);
      while (p < end) {
        const char* save = p;
        uint32_t next = utf8_decode_char(p, end);
        if (next < 0x80 && std::isalpha(static_cast<char>(next))) {
          word += static_cast<char>(next);
        } else {
          p = save;  // put back
          break;
        }
      }
      auto en_phs = EnglishG2P(word);
      phonemes.insert(phonemes.end(), en_phs.begin(), en_phs.end());
    }
  }

  return phonemes;
}

std::vector<std::string> TextFrontend::EnglishG2P(const std::string& text) const {
  std::vector<std::string> phonemes;
  std::istringstream iss(text);
  std::string token;

  while (iss >> token) {
    // Strip punctuation from edges
    std::string punct_after;
    while (!token.empty() && std::ispunct(token.back())) {
      punct_after = std::string(1, token.back()) + punct_after;
      token.pop_back();
    }
    while (!token.empty() && std::ispunct(token.front())) {
      token.erase(token.begin());
    }

    if (token.empty()) {
      if (!punct_after.empty()) {
        for (char c : punct_after) phonemes.push_back(std::string(1, c));
      }
      continue;
    }

    std::string lower = to_lower(token);
    auto it = en_lexicon_.find(lower);
    if (it != en_lexicon_.end()) {
      phonemes.insert(phonemes.end(), it->second.begin(), it->second.end());
    } else {
      // Rule-based fallback
      auto fallback = EnglishRuleFallback(lower);
      phonemes.insert(phonemes.end(), fallback.begin(), fallback.end());
    }

    // Add punctuation
    for (char c : punct_after) {
      phonemes.push_back(std::string(1, c));
    }

    phonemes.push_back("sp");  // word boundary
  }

  return phonemes;
}

std::vector<std::string> TextFrontend::EnglishRuleFallback(const std::string& word) const {
  // Very simplified letter-to-phoneme rules
  // In production, use a proper rule-based G2P or neural G2P
  std::vector<std::string> phonemes;
  for (size_t i = 0; i < word.size(); i++) {
    char c = word[i];
    switch (c) {
      case 'a': phonemes.push_back("AE"); break;
      case 'e': phonemes.push_back("EH"); break;
      case 'i': phonemes.push_back("IH"); break;
      case 'o': phonemes.push_back("AA"); break;
      case 'u': phonemes.push_back("AH"); break;
      case 'b': phonemes.push_back("B"); break;
      case 'c': phonemes.push_back("K"); break;
      case 'd': phonemes.push_back("D"); break;
      case 'f': phonemes.push_back("F"); break;
      case 'g': phonemes.push_back("G"); break;
      case 'h': phonemes.push_back("HH"); break;
      case 'j': phonemes.push_back("JH"); break;
      case 'k': phonemes.push_back("K"); break;
      case 'l': phonemes.push_back("L"); break;
      case 'm': phonemes.push_back("M"); break;
      case 'n': phonemes.push_back("N"); break;
      case 'p': phonemes.push_back("P"); break;
      case 'q': phonemes.push_back("K"); break;
      case 'r': phonemes.push_back("R"); break;
      case 's': phonemes.push_back("S"); break;
      case 't': phonemes.push_back("T"); break;
      case 'v': phonemes.push_back("V"); break;
      case 'w': phonemes.push_back("W"); break;
      case 'x': phonemes.push_back("K"); phonemes.push_back("S"); break;
      case 'y': phonemes.push_back("Y"); break;
      case 'z': phonemes.push_back("Z"); break;
      default: break;
    }
  }
  return phonemes;
}

std::vector<int32_t> TextFrontend::TextToPhonemeIds(const std::string& text,
                                                     const std::string& language) const {
  std::vector<std::string> phonemes;

  if (language == "zh" || language == "ZH") {
    phonemes = ChineseG2P(text);
  } else {
    phonemes = EnglishG2P(text);
  }

  // Convert phonemes to IDs
  std::vector<int32_t> ids;
  // Add <sos>
  auto sos_it = phoneme_to_id_.find("<sos>");
  if (sos_it != phoneme_to_id_.end()) ids.push_back(sos_it->second);

  int32_t unk_id = 3;  // default <unk> ID
  auto unk_it = phoneme_to_id_.find("<unk>");
  if (unk_it != phoneme_to_id_.end()) unk_id = unk_it->second;

  for (const auto& ph : phonemes) {
    auto it = phoneme_to_id_.find(ph);
    if (it != phoneme_to_id_.end()) {
      ids.push_back(it->second);
    } else {
      ids.push_back(unk_id);
    }
  }

  // Add <eos>
  auto eos_it = phoneme_to_id_.find("<eos>");
  if (eos_it != phoneme_to_id_.end()) ids.push_back(eos_it->second);

  return ids;
}
