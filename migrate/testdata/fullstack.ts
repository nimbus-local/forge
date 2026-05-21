export default $config({
  app(input) {
    return {
      name: "my-app",
      removal: input?.stage === "production" ? "retain" : "remove",
      home: "aws",
    };
  },
  async run() {
    const table = new sst.aws.DynamoDB("UsersTable", { fields: { pk: "string" } });
    const bucket = new sst.aws.Bucket("Uploads");
    const api = new sst.aws.ApiGatewayV2("Api");
    const fn = new sst.aws.Function("MyFn", { handler: "src/index.handler", link: [table, bucket] });
    api.route("GET /", { handler: "src/get.handler" });
    api.route("POST /users", fn);
    return {
      url: api.url,
    };
  },
});
