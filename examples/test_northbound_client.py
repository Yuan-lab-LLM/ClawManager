from __future__ import annotations

import unittest
from unittest import mock

import northbound_client


class NorthboundInstanceCollectionTest(unittest.TestCase):
    def test_uses_pro_collection_for_supported_non_workbuddy_pro_runtime(self) -> None:
        self.assertEqual(
            northbound_client.instance_collection_path("opencode", "pro"),
            "/pro-instances",
        )

    def test_keeps_workbuddy_on_canonical_lite_compatibility_collection(self) -> None:
        self.assertEqual(
            northbound_client.instance_collection_path("workbuddy", "pro"),
            "/lite-instances",
        )


class NorthboundLoginTransportTest(unittest.TestCase):
    def test_challenge_request_has_no_body(self) -> None:
        client = northbound_client.NorthboundClient("https://northbound.example.com")
        challenge = {
            "challenge_id": "nbc_test",
            "nonce": "nonce",
            "encryption": {},
        }
        tokens = {"access_token": "access", "refresh_token": "refresh"}

        with (
            mock.patch.object(
                client,
                "request",
                side_effect=[
                    (challenge, {"Date": "Tue, 25 Aug 2026 02:00:00 GMT"}),
                    (tokens, {}),
                ],
            ) as request,
            mock.patch.object(
                northbound_client,
                "encrypt_credential",
                return_value="credential-jwe",
            ),
        ):
            self.assertEqual(client.login("admin", "password"), tokens)

        first_call = request.call_args_list[0]
        self.assertEqual(first_call.args, ("POST", "/api/northbound/v1/auth/challenge"))
        self.assertNotIn("body", first_call.kwargs)

    def test_logout_request_has_no_body(self) -> None:
        client = northbound_client.NorthboundClient("https://northbound.example.com")
        client.tokens = {"access_token": "access"}

        with mock.patch.object(client, "request", return_value=(None, {})) as request:
            client.authenticated_request("POST", "/auth/logout")

        request.assert_called_once_with(
            "POST",
            "/api/northbound/v1/auth/logout",
            body=None,
            headers={"Authorization": "Bearer access"},
        )


if __name__ == "__main__":
    unittest.main()
