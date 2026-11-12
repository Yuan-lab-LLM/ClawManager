#!/usr/bin/env python3
"""Read-only slot migration inventory; never prints Secret or environment values."""
import argparse
import json
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--namespace", required=True)
    parser.add_argument("--deployment", action="append", default=[])
    args = parser.parse_args()

    def get(*parts):
        return json.loads(subprocess.check_output(["kubectl", *parts, "-o", "json"]))

    nodes = get("get", "nodes")["items"]
    report = {
        "namespace": args.namespace,
        "server_version": get("version").get("serverVersion", {}).get("gitVersion"),
        "nodes": [{"name": n["metadata"]["name"], "runtime": n["status"]["nodeInfo"]["containerRuntimeVersion"],
                   "kernel": n["status"]["nodeInfo"]["kernelVersion"]} for n in nodes],
        "deployments": [], "workspace_claims": [],
        "execution_gate": "not_run: requires candidate images and actual Pod security context",
    }
    claims = set()
    for name in args.deployment:
        d = get("-n", args.namespace, "get", "deployment", name)
        template = d["spec"]["template"]
        volumes = []
        for v in template["spec"].get("volumes", []):
            if "persistentVolumeClaim" in v:
                claim = v["persistentVolumeClaim"]["claimName"]
                claims.add(claim)
                volumes.append({"name": v["name"], "kind": "pvc", "claim": claim})
            elif "nfs" in v:
                volumes.append({"name": v["name"], "kind": "direct_nfs"})
        report["deployments"].append({
            "name": name, "uid": d["metadata"]["uid"],
            "protocol": template["metadata"].get("labels", {}).get("clawmanager.io/runtime-protocol"),
            "containers": [{"name": c["name"], "image": c["image"]} for c in template["spec"]["containers"]],
            "storage": volumes,
        })
    for name in sorted(claims):
        pvc = get("-n", args.namespace, "get", "pvc", name)
        pv = get("get", "pv", pvc["spec"]["volumeName"])
        sc_name = pvc["spec"].get("storageClassName")
        sc = get("get", "storageclass", sc_name) if sc_name else {}
        longhorn = sc.get("provisioner") == "driver.longhorn.io" and pv["spec"].get("csi", {}).get("driver") == "driver.longhorn.io"
        binding = pv["spec"].get("claimRef", {})
        bound = pvc["status"].get("phase") == "Bound" and pv["status"].get("phase") == "Bound" and binding.get("uid") == pvc["metadata"]["uid"]
        rwx = all("ReadWriteMany" in x["spec"].get("accessModes", []) for x in (pvc, pv))
        report["workspace_claims"].append({"claim": name, "pv": pv["metadata"]["name"], "bound_identity": bound,
                                           "longhorn": longhorn, "rwx": rwx,
                                           "multi_node_storage_gate": bound and longhorn and rwx})
    report["migration_ready"] = False  # Inventory alone never proves execution or data-copy safety.
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
