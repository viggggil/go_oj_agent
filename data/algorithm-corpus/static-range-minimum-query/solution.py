n,q=map(int,input().split()); a=list(map(int,input().split())); table=[a]; k=1
while 1<<k<=n:
    prev=table[-1]; half=1<<(k-1); table.append([min(prev[i],prev[i+half]) for i in range(n-(1<<k)+1)]); k+=1
for _ in range(q):
    l,r=map(int,input().split()); k=(r-l+1).bit_length()-1; print(min(table[k][l-1],table[k][r-(1<<k)]))
