import mysql from "mysql2/promise";
import { env } from "./env.js";

export async function getInstanceGatewayToken(instanceId: number): Promise<string | null> {
  const connection = await mysql.createConnection({
    host: env.db.host,
    port: env.db.port,
    user: env.db.user,
    password: env.db.password,
    database: env.db.database,
  });
  try {
    const [rows] = await connection.query<{ access_token: string | null }[]>(
      "SELECT access_token FROM instances WHERE id = ? LIMIT 1",
      [instanceId],
    );
    const row = rows[0];
    const token = row?.access_token?.trim();
    return token ? token : null;
  } finally {
    await connection.end();
  }
}
