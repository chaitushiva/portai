from flask import Flask, request, render_template
from compare import run_top10_lmsys_models
import yaml

app = Flask(__name__)

@app.route("/", methods=["GET", "POST"])
def index():
    if request.method == "POST":
        prompt = request.form["prompt"]
        portkey_api_key = request.form["portkey_api_key"]
        models_raw = request.form["models"]
        keys_raw = request.form["virtual_keys"]

        models = [line.strip().split(",") for line in models_raw.strip().splitlines()]
        virtual_keys = dict(line.strip().split("=") for line in keys_raw.strip().splitlines())

        results = run_top10_lmsys_models(prompt, portkey_api_key, models, virtual_keys)
        return render_template("index.html", results=results, prompt=prompt, models=models_raw, virtual_keys=keys_raw)

    return render_template("index.html", results=None)
