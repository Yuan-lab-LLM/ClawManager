import api from './api';

export interface ClawManagerBuildInfo {
  version: string;
  commit: string;
  build_time: string;
}

let buildInfoRequest: Promise<ClawManagerBuildInfo> | null = null;

export const versionService = {
  get: async (): Promise<ClawManagerBuildInfo> => {
    if (buildInfoRequest) {
      return buildInfoRequest;
    }

    const request = api
      .get('/version')
      .then((response) => response.data.data as ClawManagerBuildInfo);
    buildInfoRequest = request;
    try {
      return await request;
    } finally {
      if (buildInfoRequest === request) {
        buildInfoRequest = null;
      }
    }
  },
};
