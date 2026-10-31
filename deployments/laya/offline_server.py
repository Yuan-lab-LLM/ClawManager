"""Use upstream HTTP API with a single, preloaded, local CPU checkpoint."""
import os


def main():
    # Fail closed: do not expose an unauthenticated inference service by accident.
    if not os.environ.get("LAYA_API_KEY", "").strip():
        raise SystemExit("LAYA_API_KEY must be supplied at runtime (not baked into the image)")
    os.environ["HF_HUB_OFFLINE"] = "1"
    os.environ["TRANSFORMERS_OFFLINE"] = "1"
    os.environ["LAYA_DEVICE"] = "cpu"

    import torch
    import uvicorn
    from laya import Router
    from laya.serve import create_app

    threads = int(os.environ.get("LAYA_THREADS", "4"))
    if not 1 <= threads <= 32:
        raise SystemExit("LAYA_THREADS must be between 1 and 32")
    torch.set_num_threads(threads)

    class MultilingualRouter(Router):
        def predict(self, state, questions, *, model=None, **kwargs):
            if model not in (None, "multilingual"):
                raise ValueError("This offline service only contains the multilingual checkpoint")
            return super().predict(state, questions, model="multilingual", **kwargs)

    router = MultilingualRouter(
        models={"multilingual": "/opt/models/laya-multilingual"},
        device="cpu", default="multilingual", max_loaded=1,
        auto_task_detection=False,
    )
    router.preload(["multilingual"])
    uvicorn.run(create_app(router=router), host="0.0.0.0", port=8000)


if __name__ == "__main__":
    main()
