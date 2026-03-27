import json

with open("models/moonshine-tiny/tokenizer.json", "r", encoding="utf-8") as f:
    tok = json.load(f)

vocab = tok.get("model", {}).get("vocab", {})
added = tok.get("added_tokens", [])
print(f"vocab keys: {len(vocab)}")
print(f"added_tokens: {len(added)}")

if added:
    for t in added[:5]:
        print(f"  added: id={t.get('id')}, content={repr(t.get('content'))}")

if vocab:
    items = sorted(vocab.items(), key=lambda x: x[1])
    for tok_str, tok_id in items[:10]:
        print(f"  vocab[{tok_id}] = {repr(tok_str)}")
    print("  ...")
    for tok_str, tok_id in items[-3:]:
        print(f"  vocab[{tok_id}] = {repr(tok_str)}")

# Check what convert script produces
from gguf import GGUFReader
r = GGUFReader("models/gguf/moonshine-tiny.gguf")
for kv in r.fields:
    if "token" in kv.lower():
        field = r.fields[kv]
        print(f"\nGGUF field: {kv}")
        if hasattr(field, 'parts') and len(field.parts) > 1:
            # Try to read first few string values
            data = field.parts[-1]
            if hasattr(data, 'tolist'):
                vals = data.tolist()[:5]
                print(f"  first values: {vals}")
            print(f"  total parts: {len(field.parts)}")
