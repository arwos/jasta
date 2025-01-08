import { Injectable } from '@angular/core';
import { RequestService } from '@onega-ui/core';
import { Observable } from 'rxjs';
import { Config, ConfigName } from './models';

@Injectable({
  providedIn: 'root',
  deps:[RequestService],
})
export class ConfigService {

  constructor(
    private readonly req: RequestService,
  ) { }

  create(c: Config): Observable<Config> {
    return this.req.post<Config>('/create', c);
  }

  list(): Observable<ConfigName[]> {
    return this.req.get<ConfigName[]>('/load');
  }

}
