const defaultUrl = "http://localhost:3000/";
const targetUrl = process.env.URL ?? defaultUrl;
const runsInput = process.env.RUNS ?? "3";
const numberOfRuns = Number(runsInput);

let parsedUrl;
try {
  parsedUrl = new URL(targetUrl);
} catch {
  throw new Error(`Invalid Lighthouse target URL: ${targetUrl}`);
}

if (!["http:", "https:"].includes(parsedUrl.protocol)) {
  throw new Error("Lighthouse target URL must use HTTP or HTTPS");
}

if (!Number.isInteger(numberOfRuns) || numberOfRuns < 1) {
  throw new Error(`Lighthouse run count must be a positive integer: ${runsInput}`);
}

const collect = {
  url: [parsedUrl.href],
  numberOfRuns,
  settings: {
    formFactor: "mobile",
    onlyCategories: ["performance"],
  },
};

if (process.env.CHROME_BIN) {
  collect.chromePath = process.env.CHROME_BIN;
}

module.exports = {
  ci: {
    collect,
    assert: {
      assertions: {
        "largest-contentful-paint": [
          "error",
          { aggregationMethod: "median-run", maxNumericValue: 2500 },
        ],
        "cumulative-layout-shift": [
          "error",
          { aggregationMethod: "median-run", maxNumericValue: 0.1 },
        ],
        "total-blocking-time": [
          "error",
          { aggregationMethod: "median-run", maxNumericValue: 200 },
        ],
      },
    },
    upload: {
      target: "filesystem",
      outputDir: ".lighthouseci/reports",
    },
  },
};
