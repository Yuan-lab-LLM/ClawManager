"""Offline prompt-length check; needs tokenizers, never loads model weights/network.

Pass the deployed multilingual snapshot's tokenizer.json. Mirrors Laya's
choice option rendering and shared head budget (common.build_sequence).
This verifies token budgets, NOT classification accuracy or injection resistance.
"""
import argparse

from call_laya import HEAD_MAX_LEN, QUESTIONS, TIER_PROMPT_VERSION


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tokenizer", required=True, help="Local tokenizer.json path")
    args = parser.parse_args()
    from tokenizers import Tokenizer

    tokenizer = Tokenizer.from_file(args.tokenizer)
    tokenizer.no_truncation()
    tokenizer.no_padding()
    failed = False
    print("tier rubric:", TIER_PROMPT_VERSION)
    for name, question in QUESTIONS.items():
        head = len(tokenizer.encode("choice question: " + question["instructions"], add_special_tokens=False).ids)
        options = {key: len(tokenizer.encode(" " + key + ": " + value, add_special_tokens=False).ids)
                   for key, value in question["criteria"].items()}
        total = head + sum(n + 1 for n in options.values())
        valid = all(n <= 48 for n in options.values()) and total <= HEAD_MAX_LEN
        # Adapter permits <=1200 UTF-8 bytes of state; reserve at least that many
        # tokens as a conservative guard, not a universal tokenizer guarantee.
        valid = valid and total + 4 + 1200 <= 2048
        print(f"{name}: instructions={head}, options={options}, head={total}/{HEAD_MAX_LEN}, valid={valid}")
        failed |= not valid
    return int(failed)


if __name__ == "__main__":
    raise SystemExit(main())
