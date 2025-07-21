from openai import OpenAI
from portkey_ai import PORTKEY_GATEWAY_URL, createHeaders

def run_top10_lmsys_models(prompt, portkey_api_key, top_10_models, virtual_keys):
    outputs = {}

    for model, provider in top_10_models:
        if provider not in virtual_keys:
            outputs[model] = f"[ERROR] Missing virtual key for provider: {provider}"
            continue

        try:
            portkey = OpenAI(
                api_key="dummy_key",
                base_url=PORTKEY_GATEWAY_URL,
                default_headers=createHeaders(
                    api_key=portkey_api_key,
                    virtual_key=virtual_keys[provider],
                    trace_id="UI_COMPARISON"
                )
            )

            response = portkey.chat.completions.create(
                messages=[{"role": "user", "content": prompt}],
                model=model,
                max_tokens=256
            )

            outputs[model] = response.choices[0].message.content

        except Exception as e:
            outputs[model] = f"[ERROR] {str(e)}"

    return outputs
