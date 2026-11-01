import http from "node:http";
import { readFile } from "node:fs/promises";
import path from "node:path";
const root = path.resolve("frontend");
http
  .createServer(async (req, res) => {
    try {
      const file = path.resolve(
        root,
        "." + (req.url === "/" ? "/index.html" : req.url.split("?")[0]),
      );
      if (!file.startsWith(root + path.sep)) throw Error();
      const data = await readFile(file);
      res.setHeader(
        "Content-Type",
        file.endsWith(".js")
          ? "text/javascript"
          : file.endsWith(".css")
            ? "text/css"
            : "text/html",
      );
      res.end(data);
    } catch {
      res.statusCode = 404;
      res.end();
    }
  })
  .listen(4179, "127.0.0.1");
